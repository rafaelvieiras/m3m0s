package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/auth"
)

// Account endpoints (D35, docs/api-contract.md, "Accounts" section). Browser
// sessions use an HttpOnly cookie; every state-changing request authenticated
// by that cookie must also carry csrfHeader, which a cross-site form cannot
// set. API keys remain the credential for machines and for admin operations.
const (
	sessionCookie = "m3m0s_session"
	csrfHeader    = "X-M3m0s-Csrf"
	maxAuthBody   = 8 << 10
)

func toAccountUser(u auth.User) apitypes.AccountUser {
	return apitypes.AccountUser{Username: u.Username, HasPassword: u.HasPassword, CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt}
}

// clientIP is the address used for login throttling: the last
// X-Forwarded-For entry (appended by the reverse proxy in front of serve),
// else the socket peer.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: secureRequest(r),
		SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func sessionToken(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func hasCSRF(r *http.Request) bool { return r.Header.Get(csrfHeader) != "" }

func (s *Server) validAPIKey(r *http.Request) bool {
	token, ok := auth.BearerToken(r.Header.Get("Authorization"))
	return ok && s.keys.Verify(token)
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return false
	}
	return true
}

// writeAccountError maps an auth error to a response; it reports whether err was handled.
func writeAccountError(w http.ResponseWriter, err error) {
	var verr *auth.ValidationError
	var terr *auth.ThrottledError
	switch {
	case errors.As(err, &verr):
		code := "invalid_password"
		if strings.HasPrefix(verr.Message, "username") {
			code = "invalid_username"
		}
		writeError(w, http.StatusBadRequest, code, verr.Message)
	case errors.As(err, &terr):
		w.Header().Set("Retry-After", strconv.Itoa(int(terr.RetryAfter.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "too_many_attempts", terr.Error())
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
	case errors.Is(err, auth.ErrInvalidToken):
		writeError(w, http.StatusNotFound, "invalid_link", "this link is invalid, expired or already used")
	case errors.Is(err, auth.ErrUserExists):
		writeError(w, http.StatusConflict, "username_taken", "that username already exists")
	case errors.Is(err, auth.ErrUsersExist):
		writeError(w, http.StatusConflict, "account_exists", "an account already exists; sign in instead")
	case errors.Is(err, auth.ErrNoSuchUser):
		writeError(w, http.StatusNotFound, "no_such_user", "no such account")
	default:
		writeError(w, http.StatusInternalServerError, "account_failed", "internal error")
	}
}

func passwordsMatch(password, confirm string) bool { return password == confirm }

// ── GET /auth/status ──

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	has, err := s.accounts.HasAccounts(r.Context())
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.AuthStatusResponse{NeedsOnboarding: !has})
}

// ── POST /auth/onboard ──

// handleOnboard creates the first account. The API is reachable before any
// account exists, so the caller must prove control of the instance with an API
// key (the bootstrap key printed at first boot); a stranger cannot claim it.
func (s *Server) handleOnboard(w http.ResponseWriter, r *http.Request) {
	if !s.validAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "onboarding requires an API key")
		return
	}
	var req apitypes.OnboardRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !passwordsMatch(req.Password, req.Confirm) {
		writeError(w, http.StatusBadRequest, "password_mismatch", "password and confirmation do not match")
		return
	}
	if _, err := s.accounts.Onboard(r.Context(), req.Username, req.Password); err != nil {
		writeAccountError(w, err)
		return
	}
	user, token, err := s.accounts.Login(r.Context(), req.Username, req.Password, clientIP(r), r.UserAgent())
	if err != nil {
		writeAccountError(w, err)
		return
	}
	setSessionCookie(w, r, token, auth.SessionTTL)
	writeJSON(w, http.StatusCreated, toAccountUser(user))
}

// ── POST /auth/login, POST /auth/logout, GET /auth/me ──

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !hasCSRF(r) {
		writeError(w, http.StatusForbidden, "csrf", "missing "+csrfHeader+" header")
		return
	}
	var req apitypes.LoginRequest
	if !decodeBody(w, r, &req) {
		return
	}
	user, token, err := s.accounts.Login(r.Context(), req.Username, req.Password, clientIP(r), r.UserAgent())
	if err != nil {
		writeAccountError(w, err)
		return
	}
	setSessionCookie(w, r, token, auth.SessionTTL)
	writeJSON(w, http.StatusOK, toAccountUser(user))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !hasCSRF(r) {
		writeError(w, http.StatusForbidden, "csrf", "missing "+csrfHeader+" header")
		return
	}
	s.accounts.Logout(r.Context(), sessionToken(r))
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if s.validAPIKey(r) {
		writeJSON(w, http.StatusOK, apitypes.MeResponse{Via: "apikey"})
		return
	}
	if u, ok := s.accounts.SessionUser(r.Context(), sessionToken(r)); ok {
		writeJSON(w, http.StatusOK, apitypes.MeResponse{Via: "session", Username: u.Username})
		return
	}
	writeError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
}

// ── GET/POST /auth/reset ──

func (s *Server) handleResetInfo(w http.ResponseWriter, r *http.Request) {
	u, kind, err := s.accounts.ResetInfo(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.ResetInfoResponse{Username: u.Username, Kind: kind})
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req apitypes.ResetPasswordRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !passwordsMatch(req.Password, req.Confirm) {
		writeError(w, http.StatusBadRequest, "password_mismatch", "password and confirmation do not match")
		return
	}
	if _, err := s.accounts.CompleteReset(r.Context(), req.Token, req.Password); err != nil {
		writeAccountError(w, err)
		return
	}
	// Every session of the account was revoked; the owner signs in again.
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// ── admin (API key only): /users ──

func (s *Server) handleUsersList(w http.ResponseWriter, r *http.Request) {
	users, err := s.accounts.List(r.Context())
	if err != nil {
		writeAccountError(w, err)
		return
	}
	out := apitypes.UsersResponse{Users: make([]apitypes.AccountUser, len(users))}
	for i, u := range users {
		out.Users[i] = toAccountUser(u)
	}
	writeJSON(w, http.StatusOK, out)
}

func resetLink(user auth.User, kind, token string, exp time.Time) apitypes.ResetLinkResponse {
	return apitypes.ResetLinkResponse{
		User: toAccountUser(user), Kind: kind, Token: token, ExpiresAt: exp,
		Path: "/reset-password?token=" + url.QueryEscape(token),
	}
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	var req apitypes.CreateUserRequest
	if !decodeBody(w, r, &req) {
		return
	}
	user, token, exp, err := s.accounts.Invite(r.Context(), req.Username)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resetLink(user, "invite", token, exp))
}

func (s *Server) handleUserReset(w http.ResponseWriter, r *http.Request) {
	user, token, exp, err := s.accounts.StartReset(r.Context(), r.PathValue("username"))
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resetLink(user, "reset", token, exp))
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.accounts.Delete(r.Context(), r.PathValue("username")); err != nil {
		writeAccountError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

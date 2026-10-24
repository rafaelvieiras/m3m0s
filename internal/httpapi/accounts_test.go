package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

type acctEnv struct {
	t       *testing.T
	url     string
	apiKey  string
	browser *http.Client // keeps the session cookie, like a real browser
}

func newAcctEnv(t *testing.T) *acctEnv {
	t.Helper()
	srv, rawKey := newTestServer(t)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	jar, _ := cookiejar.New(nil)
	return &acctEnv{t: t, url: hs.URL, apiKey: rawKey, browser: &http.Client{Jar: jar}}
}

// do sends a request; as is "key" (API key), "browser" (cookie jar) or "" (anonymous).
func (e *acctEnv) do(as, method, path, body string, hdr ...string) (int, []byte, http.Header) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.url+path, strings.NewReader(body))
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	client := http.DefaultClient
	switch as {
	case "key":
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	case "browser":
		client = e.browser
	}
	resp, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, resp.Header
}

func (e *acctEnv) expect(want int, as, method, path, body string, hdr ...string) []byte {
	e.t.Helper()
	got, raw, _ := e.do(as, method, path, body, hdr...)
	if got != want {
		e.t.Fatalf("%s %s as %q: status %d, want %d (%s)", method, path, as, got, want, raw)
	}
	return raw
}

const csrf = "X-M3m0s-Csrf"
const pw = "a-long-enough-pass"

func TestAccountsOnboardingLoginAndSessions(t *testing.T) {
	e := newAcctEnv(t)

	var st apitypes.AuthStatusResponse
	_ = json.Unmarshal(e.expect(200, "", "GET", "/api/v1/auth/status", ""), &st)
	if !st.NeedsOnboarding {
		t.Fatal("fresh instance must need onboarding")
	}

	onboard := `{"username":"admin","password":"` + pw + `","confirm":"` + pw + `"}`
	e.expect(403, "", "POST", "/api/v1/auth/onboard", onboard) // no CSRF header: a cross-site form cannot claim the instance
	e.expect(400, "key", "POST", "/api/v1/auth/onboard", `{"username":"admin","password":"`+pw+`","confirm":"different-pass-x"}`, csrf, "1")
	e.expect(400, "key", "POST", "/api/v1/auth/onboard", `{"username":"admin","password":"short","confirm":"short"}`, csrf, "1")
	e.expect(400, "key", "POST", "/api/v1/auth/onboard", `{"username":"A B","password":"`+pw+`","confirm":"`+pw+`"}`, csrf, "1")
	e.expect(201, "browser", "POST", "/api/v1/auth/onboard", onboard, csrf, "1")
	e.expect(409, "key", "POST", "/api/v1/auth/onboard", onboard, csrf, "1")
	_ = json.Unmarshal(e.expect(200, "", "GET", "/api/v1/auth/status", ""), &st)
	if st.NeedsOnboarding {
		t.Fatal("onboarding still offered after the first account")
	}

	// onboarding signed the browser in
	var me apitypes.MeResponse
	_ = json.Unmarshal(e.expect(200, "browser", "GET", "/api/v1/auth/me", ""), &me)
	if me.Via != "session" || me.Username != "admin" {
		t.Fatalf("me = %+v", me)
	}
	e.expect(200, "browser", "GET", "/api/v1/stats", "")
	// state-changing requests by cookie need the CSRF header
	e.expect(403, "browser", "POST", "/api/v1/search", `{"query":"x","mode":"keyword"}`)
	e.expect(200, "browser", "POST", "/api/v1/search", `{"query":"x","mode":"keyword"}`, csrf, "1")
	// the cookie is not an API key: admin routes refuse it
	e.expect(401, "browser", "GET", "/api/v1/users", "")
	e.expect(401, "", "GET", "/api/v1/stats", "")
	e.expect(200, "key", "GET", "/api/v1/stats", "") // API keys keep working

	// cookie flags
	_, _, h := e.do("", "POST", "/api/v1/auth/login", `{"username":"admin","password":"`+pw+`"}`, csrf, "1")
	c := h.Get("Set-Cookie")
	if !strings.Contains(c, "HttpOnly") || !strings.Contains(c, "SameSite=Lax") || !strings.Contains(c, "m3m0s_session=") {
		t.Errorf("session cookie flags: %s", c)
	}
	if _, _, h := e.do("", "POST", "/api/v1/auth/login", `{"username":"admin","password":"`+pw+`"}`, csrf, "1", "X-Forwarded-Proto", "https"); !strings.Contains(h.Get("Set-Cookie"), "Secure") {
		t.Error("cookie must be Secure behind an https proxy")
	}

	// login failures, CSRF on login, logout
	e.expect(403, "", "POST", "/api/v1/auth/login", `{"username":"admin","password":"`+pw+`"}`)
	e.expect(401, "", "POST", "/api/v1/auth/login", `{"username":"admin","password":"nope-nope-nope"}`, csrf, "1")
	e.expect(401, "", "POST", "/api/v1/auth/login", `{"username":"ghost","password":"nope-nope-nope"}`, csrf, "1")
	e.expect(204, "browser", "POST", "/api/v1/auth/logout", "", csrf, "1")
	e.expect(401, "browser", "GET", "/api/v1/auth/me", "")
}

func TestAccountsLoginThrottle(t *testing.T) {
	e := newAcctEnv(t)
	e.expect(201, "key", "POST", "/api/v1/auth/onboard", `{"username":"admin","password":"`+pw+`","confirm":"`+pw+`"}`, csrf, "1")
	for i := 0; i < 6; i++ {
		e.do("", "POST", "/api/v1/auth/login", `{"username":"admin","password":"bad-bad-bad-bad"}`, csrf, "1")
	}
	code, raw, h := e.do("", "POST", "/api/v1/auth/login", `{"username":"admin","password":"`+pw+`"}`, csrf, "1")
	if code != 429 || h.Get("Retry-After") == "" || !strings.Contains(string(raw), "too_many_attempts") {
		t.Fatalf("throttle: %d %s retry=%q", code, raw, h.Get("Retry-After"))
	}
}

func TestAccountsInviteAndCLIResetFlow(t *testing.T) {
	e := newAcctEnv(t)
	e.expect(201, "key", "POST", "/api/v1/auth/onboard", `{"username":"admin","password":"`+pw+`","confirm":"`+pw+`"}`, csrf, "1")

	// admin routes need an API key
	e.expect(401, "", "POST", "/api/v1/users", `{"username":"alice"}`)
	var inv apitypes.ResetLinkResponse
	_ = json.Unmarshal(e.expect(201, "key", "POST", "/api/v1/users", `{"username":"alice"}`), &inv)
	if inv.Kind != "invite" || inv.User.HasPassword || !strings.HasPrefix(inv.Path, "/reset-password?token=") || inv.Token == "" {
		t.Fatalf("invite = %+v", inv)
	}
	e.expect(409, "key", "POST", "/api/v1/users", `{"username":"alice"}`)

	// the reset page: validate, then set the password twice
	var info apitypes.ResetInfoResponse
	_ = json.Unmarshal(e.expect(200, "", "GET", "/api/v1/auth/reset?token="+inv.Token, ""), &info)
	if info.Username != "alice" || info.Kind != "invite" {
		t.Fatalf("info = %+v", info)
	}
	e.expect(404, "", "GET", "/api/v1/auth/reset?token=bogus", "")
	body := func(p, c string) string {
		return `{"token":"` + inv.Token + `","password":"` + p + `","confirm":"` + c + `"}`
	}
	e.expect(400, "", "POST", "/api/v1/auth/reset", body("alices-long-password", "alices-other-password"))
	e.expect(400, "", "POST", "/api/v1/auth/reset", body("short", "short"))
	e.expect(204, "", "POST", "/api/v1/auth/reset", body("alices-long-password", "alices-long-password"))
	e.expect(404, "", "POST", "/api/v1/auth/reset", body("alices-long-password", "alices-long-password")) // single use

	// alice signs in; the CLI later resets her password: her session dies on completion
	e.expect(200, "browser", "POST", "/api/v1/auth/login", `{"username":"alice","password":"alices-long-password"}`, csrf, "1")
	e.expect(200, "browser", "GET", "/api/v1/stats", "")
	var rl apitypes.ResetLinkResponse
	e.expect(404, "key", "POST", "/api/v1/users/ghost/reset", "")
	_ = json.Unmarshal(e.expect(200, "key", "POST", "/api/v1/users/alice/reset", ""), &rl)
	if rl.Kind != "reset" || !rl.User.HasPassword {
		t.Fatalf("reset link = %+v", rl)
	}
	e.expect(200, "browser", "GET", "/api/v1/stats", "") // still signed in until the reset completes
	e.expect(204, "", "POST", "/api/v1/auth/reset", `{"token":"`+rl.Token+`","password":"alices-new-password","confirm":"alices-new-password"}`)
	e.expect(401, "browser", "GET", "/api/v1/stats", "")
	e.expect(401, "", "POST", "/api/v1/auth/login", `{"username":"alice","password":"alices-long-password"}`, csrf, "1")
	e.expect(200, "", "POST", "/api/v1/auth/login", `{"username":"alice","password":"alices-new-password"}`, csrf, "1")

	var list apitypes.UsersResponse
	_ = json.Unmarshal(e.expect(200, "key", "GET", "/api/v1/users", ""), &list)
	if len(list.Users) != 2 {
		t.Fatalf("users = %+v", list.Users)
	}
	e.expect(204, "key", "DELETE", "/api/v1/users/alice", "")
	e.expect(404, "key", "DELETE", "/api/v1/users/alice", "")
}

func TestKeyCreate(t *testing.T) {
	e := newAcctEnv(t)
	e.expect(401, "", "POST", "/api/v1/keys", `{}`)

	// an API key can mint another one, and the new key works
	var out apitypes.CreateKeyResponse
	_ = json.Unmarshal(e.expect(201, "key", "POST", "/api/v1/keys", `{"label":"laptop"}`), &out)
	if !strings.HasPrefix(out.Key, "m3m0s_") || out.Label != "laptop" || !strings.HasPrefix(out.Key, out.Prefix) {
		t.Fatalf("response = %+v", out)
	}
	if out.Key == e.apiKey {
		t.Fatal("minted key equals the existing one")
	}
	req, _ := http.NewRequest("GET", e.url+"/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer "+out.Key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("minted key rejected: %d", resp.StatusCode)
	}

	// default label, label length limit
	_ = json.Unmarshal(e.expect(201, "key", "POST", "/api/v1/keys", ``), &out)
	if out.Label != "webui" {
		t.Errorf("default label = %q", out.Label)
	}
	e.expect(400, "key", "POST", "/api/v1/keys", `{"label":"`+strings.Repeat("x", 65)+`"}`)
	e.expect(400, "key", "POST", "/api/v1/keys", `not json`)

	// a signed-in session can mint one, but only with the CSRF header
	onboard := `{"username":"admin","password":"` + pw + `","confirm":"` + pw + `"}`
	e.expect(201, "browser", "POST", "/api/v1/auth/onboard", onboard, csrf, "1")
	e.expect(403, "browser", "POST", "/api/v1/keys", `{}`)
	e.expect(201, "browser", "POST", "/api/v1/keys", `{}`, csrf, "1")
}

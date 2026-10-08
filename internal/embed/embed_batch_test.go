package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewBatchOrdersByIndexAndSendsDimensions(t *testing.T) {
	var gotAuth string
	var gotDims float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotDims, _ = req["dimensions"].(float64)
		// Out of order on purpose.
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[2]},{"index":0,"embedding":[1]}]}`))
	}))
	defer srv.Close()
	out, err := NewBatch(srv.URL, "k", "m", 1)(context.Background(), []string{"a", "b"})
	if err != nil || len(out) != 2 || out[0][0] != 1 || out[1][0] != 2 || gotAuth != "Bearer k" || gotDims != 1 {
		t.Fatalf("out=%v err=%v auth=%q dims=%v", out, err, gotAuth, gotDims)
	}
}

func TestBatchErrorClassification(t *testing.T) {
	for code, want := range map[int][2]bool{ // transient, fatal
		http.StatusTooManyRequests: {true, false}, http.StatusBadGateway: {true, false},
		http.StatusUnauthorized: {false, true}, http.StatusNotFound: {false, true},
		http.StatusBadRequest: {false, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
		_, err := NewBatch(srv.URL, "secret-key", "m", 0)(context.Background(), []string{"a"})
		srv.Close()
		if IsTransient(err) != want[0] || IsFatal(err) != want[1] {
			t.Errorf("%d: transient=%v fatal=%v, want %v", code, IsTransient(err), IsFatal(err), want)
		}
		if strings.Contains(err.Error(), "secret-key") {
			t.Errorf("error leaks the key: %v", err)
		}
	}
	if IsTransient(context.Canceled) {
		t.Error("cancellation is not transient")
	}
	// A refused connection is transient.
	_, err := NewBatch("http://127.0.0.1:1", "k", "m", 0)(context.Background(), []string{"a"})
	if !IsTransient(err) {
		t.Errorf("connection error should be transient: %v", err)
	}
}

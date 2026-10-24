package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/store"
)

// trashCall performs an authenticated request against the test server and
// decodes the JSON body into out (when non-nil).
func trashCall(t *testing.T, base, key, method, path string, out any) int {
	t.Helper()
	req, err := http.NewRequest(method, base+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return resp.StatusCode
}

func addForTrash(t *testing.T, srv *Server, content, domain string, md map[string]string) string {
	t.Helper()
	out, err := srv.store.Add(context.Background(), store.AddInput{Content: content, Domain: domain, Metadata: md}, time.Now())
	if err != nil || len(out.Entries) != 1 {
		t.Fatalf("Add: %v (%d entries)", err, len(out.Entries))
	}
	return out.Entries[0].Entry.ID
}

func TestDeleteByIDIsSoftThenRestorable(t *testing.T) {
	srv, key := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	id := addForTrash(t, srv, "soft delete via http", "code:http-trash-a", nil)

	var del apitypes.DeleteByIDResponse
	if code := trashCall(t, ts.URL, key, http.MethodDelete, "/api/v1/memories/"+id, &del); code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200", code)
	}
	if del.ID != id || del.Purged || del.DeletedAt == "" || del.PurgeAt == "" {
		t.Errorf("unexpected response: %+v", del)
	}
	if code := trashCall(t, ts.URL, key, http.MethodGet, "/api/v1/memories/"+id, nil); code != http.StatusNotFound {
		t.Errorf("GET after soft delete = %d, want 404", code)
	}

	var trash apitypes.TrashResponse
	if code := trashCall(t, ts.URL, key, http.MethodGet, "/api/v1/memories/trash", &trash); code != http.StatusOK {
		t.Fatalf("GET trash = %d", code)
	}
	if len(trash.Items) != 1 || trash.Items[0].ID != id || trash.GraceDays != store.SoftDeleteGraceDays {
		t.Errorf("unexpected trash: %+v", trash)
	}
	if trash.Items[0].Snippet != "soft delete via http" || trash.Items[0].PurgeAt == "" {
		t.Errorf("unexpected trash item: %+v", trash.Items[0])
	}

	if code := trashCall(t, ts.URL, key, http.MethodPost, "/api/v1/memories/"+id+"/restore", nil); code != http.StatusOK {
		t.Fatalf("restore = %d, want 200", code)
	}
	if code := trashCall(t, ts.URL, key, http.MethodGet, "/api/v1/memories/"+id, nil); code != http.StatusOK {
		t.Errorf("GET after restore = %d, want 200", code)
	}
	if code := trashCall(t, ts.URL, key, http.MethodDelete, "/api/v1/memories/mem_nope", nil); code != http.StatusNotFound {
		t.Errorf("DELETE unknown id = %d, want 404", code)
	}
}

func TestDeleteByIDPurgeIsHard(t *testing.T) {
	srv, key := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	id := addForTrash(t, srv, "sensitive content", "code:http-trash-b", nil)

	var del apitypes.DeleteByIDResponse
	if code := trashCall(t, ts.URL, key, http.MethodDelete, "/api/v1/memories/"+id+"?purge=true", &del); code != http.StatusOK {
		t.Fatalf("DELETE purge = %d, want 200", code)
	}
	if !del.Purged {
		t.Errorf("Purged = false: %+v", del)
	}
	if code := trashCall(t, ts.URL, key, http.MethodPost, "/api/v1/memories/"+id+"/restore", nil); code != http.StatusNotFound {
		t.Errorf("restore after purge = %d, want 404", code)
	}
	var trash apitypes.TrashResponse
	trashCall(t, ts.URL, key, http.MethodGet, "/api/v1/memories/trash", &trash)
	if len(trash.Items) != 0 {
		t.Errorf("purged entry listed in the trash: %+v", trash.Items)
	}
	if code := trashCall(t, ts.URL, key, http.MethodDelete, "/api/v1/memories/x?purge=maybe", nil); code != http.StatusBadRequest {
		t.Errorf("invalid purge value = %d, want 400", code)
	}
}

func TestTrashPurgeOneAndEmpty(t *testing.T) {
	srv, key := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	a := addForTrash(t, srv, "a", "code:http-trash-c", nil)
	b := addForTrash(t, srv, "b", "code:http-trash-d", nil)
	live := addForTrash(t, srv, "c", "code:http-trash-e", nil)
	for _, id := range []string{a, b} {
		if code := trashCall(t, ts.URL, key, http.MethodDelete, "/api/v1/memories/"+id, nil); code != http.StatusOK {
			t.Fatalf("DELETE %s = %d", id, code)
		}
	}

	// A live entry cannot be purged through the trash endpoint.
	if code := trashCall(t, ts.URL, key, http.MethodDelete, "/api/v1/memories/trash/"+live, nil); code != http.StatusNotFound {
		t.Errorf("purge of a live entry = %d, want 404", code)
	}
	if code := trashCall(t, ts.URL, key, http.MethodDelete, "/api/v1/memories/trash/"+a, nil); code != http.StatusOK {
		t.Errorf("purge of a trashed entry = %d, want 200", code)
	}

	var empty apitypes.EmptyTrashResponse
	if code := trashCall(t, ts.URL, key, http.MethodDelete, "/api/v1/memories/trash", &empty); code != http.StatusOK {
		t.Fatalf("empty trash = %d", code)
	}
	if empty.PurgedCount != 1 {
		t.Errorf("PurgedCount = %d, want 1 (a was already purged)", empty.PurgedCount)
	}
	if code := trashCall(t, ts.URL, key, http.MethodGet, "/api/v1/memories/"+live, nil); code != http.StatusOK {
		t.Errorf("live entry must survive: GET = %d", code)
	}
}

func TestDeleteByFilePathStaysHard(t *testing.T) {
	srv, key := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	id := addForTrash(t, srv, "watched chunk", "code:http-trash-f", map[string]string{"file_path": "docs/x.md"})

	var out apitypes.DeleteByFilePathResponse
	if code := trashCall(t, ts.URL, key, http.MethodDelete, "/api/v1/memories?domain=code:http-trash-f&filePath=docs/x.md", &out); code != http.StatusOK {
		t.Fatalf("DELETE by file path = %d", code)
	}
	if out.DeletedCount != 1 {
		t.Errorf("DeletedCount = %d, want 1", out.DeletedCount)
	}
	if code := trashCall(t, ts.URL, key, http.MethodPost, "/api/v1/memories/"+id+"/restore", nil); code != http.StatusNotFound {
		t.Errorf("restore after file-path delete = %d, want 404 (hard delete)", code)
	}
}

func TestTrashRequiresAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/memories/trash"},
		{http.MethodDelete, "/api/v1/memories/trash"},
		{http.MethodDelete, "/api/v1/memories/trash/abc"},
	} {
		if code := trashCall(t, ts.URL, "wrong-key", c.method, c.path, nil); code != http.StatusUnauthorized {
			t.Errorf("%s %s with a bad key = %d, want 401", c.method, c.path, code)
		}
	}
}

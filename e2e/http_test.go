//go:build e2e

package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
)

func TestEndToEndHTTP(t *testing.T) {
	embed := newFakeEmbedServer(t)
	srv := startServe(t, embed.URL)

	t.Run("health without auth", func(t *testing.T) {
		resp, _ := doJSON(t, http.MethodGet, srv.URL+"/health", "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("401 without key on every protected route", func(t *testing.T) {
		cases := []struct {
			method, path string
			body         any
		}{
			{http.MethodPost, "/api/v1/memories", apitypes.AddRequest{Content: "x", Domain: "code:x"}},
			{http.MethodPost, "/api/v1/search", apitypes.SearchRequest{Query: "x"}},
			{http.MethodGet, "/api/v1/recent", nil},
			{http.MethodGet, "/api/v1/stats", nil},
			{http.MethodGet, "/api/v1/memories/mem_any", nil},
		}
		for _, c := range cases {
			resp, _ := doJSON(t, c.method, srv.URL+c.path, "", c.body)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("%s %s without key: expected 401, got %d", c.method, c.path, resp.StatusCode)
			}
		}
	})

	t.Run("401 with wrong key", func(t *testing.T) {
		resp, _ := doJSON(t, http.MethodGet, srv.URL+"/api/v1/stats", "completely-wrong-key", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("200 with the bootstrap key", func(t *testing.T) {
		resp, _ := doJSON(t, http.MethodGet, srv.URL+"/api/v1/stats", srv.APIKey, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid domain returns 400 invalid_domain", func(t *testing.T) {
		resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{
			Content: "valid content",
			Domain:  "NoColonNorLowercase",
		})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", resp.StatusCode, data)
		}
		var errBody apitypes.ErrorBody
		mustUnmarshal(t, data, &errBody)
		if errBody.ErrorInfo.Code != "invalid_domain" {
			t.Fatalf("expected code invalid_domain, got %q", errBody.ErrorInfo.Code)
		}
	})

	t.Run("empty content returns 400", func(t *testing.T) {
		resp, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{
			Content: "",
			Domain:  "code:x",
		})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("full flow add -> search -> recent -> stats", func(t *testing.T) {
		addResp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{
			Content:  "M3m0s uses chromem-go for local vector search in Go",
			Domain:   "code:test-e2e-flow",
			Source:   "e2e",
			Tags:     []string{"full-flow"},
			Metadata: map[string]string{"k": "v"},
		})
		if addResp.StatusCode != http.StatusOK {
			t.Fatalf("add: status %d, body %s", addResp.StatusCode, data)
		}
		var added apitypes.AddResponse
		mustUnmarshal(t, data, &added)
		if added.Entries[0].Entry.ID == "" {
			t.Fatal("add: empty id")
		}
		if added.Entries[0].Deduped {
			t.Fatal("add: should not dedup on the first insert")
		}

		searchResp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/search", srv.APIKey, apitypes.SearchRequest{
			Query:  "vector search chromem-go in Go",
			Domain: "code:test-e2e-flow",
		})
		if searchResp.StatusCode != http.StatusOK {
			t.Fatalf("search: status %d, body %s", searchResp.StatusCode, data)
		}
		var searched apitypes.SearchResponse
		mustUnmarshal(t, data, &searched)
		if len(searched.Results) == 0 {
			t.Fatal("search: no results")
		}
		var found bool
		for _, r := range searched.Results {
			if r.ID == added.Entries[0].Entry.ID {
				found = true
				if r.Score == nil {
					t.Error("search: result without score")
				}
			}
		}
		if !found {
			t.Error("search: added memory did not appear in the results")
		}
		if !strings.Contains(searched.ContextBlock, "code:test-e2e-flow") {
			t.Errorf("search: contextBlock without the domain: %q", searched.ContextBlock)
		}

		recentResp, data := doJSON(t, http.MethodGet, srv.URL+"/api/v1/recent?domain=code:test-e2e-flow", srv.APIKey, nil)
		if recentResp.StatusCode != http.StatusOK {
			t.Fatalf("recent: status %d, body %s", recentResp.StatusCode, data)
		}
		var recent apitypes.RecentResponse
		mustUnmarshal(t, data, &recent)
		var recentFound bool
		for _, r := range recent.Results {
			if r.ID == added.Entries[0].Entry.ID {
				recentFound = true
			}
		}
		if !recentFound {
			t.Error("recent: added memory did not appear")
		}

		statsResp, data := doJSON(t, http.MethodGet, srv.URL+"/api/v1/stats", srv.APIKey, nil)
		if statsResp.StatusCode != http.StatusOK {
			t.Fatalf("stats: status %d, body %s", statsResp.StatusCode, data)
		}
		var stats apitypes.StatsResponse
		mustUnmarshal(t, data, &stats)
		if stats.Count < 1 {
			t.Errorf("stats: expected count >= 1, got %d", stats.Count)
		}
		if stats.CountByDomainCategory["code"] < 1 {
			t.Errorf("stats: expected countByDomainCategory[code] >= 1, got %d", stats.CountByDomainCategory["code"])
		}
	})

	t.Run("similarity dedup within the domain", func(t *testing.T) {
		domain := "code:test-e2e-dedup"
		content := "Text exclusive to the dedup test, unique ID ZZYYXX998877"

		r1, d1 := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{Content: content, Domain: domain})
		if r1.StatusCode != http.StatusOK {
			t.Fatalf("add 1: status %d, body %s", r1.StatusCode, d1)
		}
		var a1 apitypes.AddResponse
		mustUnmarshal(t, d1, &a1)

		r2, d2 := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{Content: content, Domain: domain})
		if r2.StatusCode != http.StatusOK {
			t.Fatalf("add 2: status %d, body %s", r2.StatusCode, d2)
		}
		var a2 apitypes.AddResponse
		mustUnmarshal(t, d2, &a2)
		if !a2.Entries[0].Deduped {
			t.Fatal("expected deduped=true for identical content in the same domain")
		}
		if a2.Entries[0].Entry.ID != a1.Entries[0].Entry.ID {
			t.Fatalf("expected the same id, got %s (original %s)", a2.Entries[0].Entry.ID, a1.Entries[0].Entry.ID)
		}

		r3, d3 := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{
			Content: "Completely different subject: traditional Italian cooking recipes with fresh pasta",
			Domain:  domain,
		})
		if r3.StatusCode != http.StatusOK {
			t.Fatalf("add 3: status %d, body %s", r3.StatusCode, d3)
		}
		var a3 apitypes.AddResponse
		mustUnmarshal(t, d3, &a3)
		if a3.Entries[0].Deduped {
			t.Fatal("should not dedup on completely unrelated content")
		}
		if a3.Entries[0].Entry.ID == a1.Entries[0].Entry.ID {
			t.Fatal("expected a new id for the non-duplicate content")
		}
	})

	t.Run("isolation by domain/domainCategory/source/agent filter", func(t *testing.T) {
		add := func(content, domain, source, agent string) apitypes.AddResponse {
			req := apitypes.AddRequest{Content: content, Domain: domain, Source: source, Agent: agent}
			resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, req)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("add filtro: status %d, body %s", resp.StatusCode, data)
			}
			var out apitypes.AddResponse
			mustUnmarshal(t, data, &out)
			return out
		}

		e1 := add("Wombat nocturnal burrowing marsupial tunnel", "code:filter-alpha", "e2e-src-alpha", "")
		e2 := add("Wombat nocturnal burrowing marsupial excavation", "code:filter-beta", "e2e-src-beta", "filter-agent")
		e3 := add("Wombat nocturnal burrowing marsupial hole", "writing:filter-gamma", "e2e-src-alpha", "")

		search := func(filter apitypes.SearchRequest) []string {
			filter.Query = "wombat nocturnal burrowing marsupial"
			filter.Limit = 20
			resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/search", srv.APIKey, filter)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("search filtro: status %d, body %s", resp.StatusCode, data)
			}
			var out apitypes.SearchResponse
			mustUnmarshal(t, data, &out)
			ids := make([]string, 0, len(out.Results))
			for _, r := range out.Results {
				ids = append(ids, r.ID)
			}
			return ids
		}
		contains := func(ids []string, id string) bool {
			for _, x := range ids {
				if x == id {
					return true
				}
			}
			return false
		}

		byDomain := search(apitypes.SearchRequest{Domain: "code:filter-alpha"})
		if !contains(byDomain, e1.Entries[0].Entry.ID) || contains(byDomain, e2.Entries[0].Entry.ID) || contains(byDomain, e3.Entries[0].Entry.ID) {
			t.Errorf("domain filter leaked: %v (expected only %s)", byDomain, e1.Entries[0].Entry.ID)
		}

		byCategory := search(apitypes.SearchRequest{DomainCategory: "code"})
		if !contains(byCategory, e1.Entries[0].Entry.ID) || !contains(byCategory, e2.Entries[0].Entry.ID) || contains(byCategory, e3.Entries[0].Entry.ID) {
			t.Errorf("wrong domainCategory filter: %v (expected %s and %s, not %s)", byCategory, e1.Entries[0].Entry.ID, e2.Entries[0].Entry.ID, e3.Entries[0].Entry.ID)
		}

		bySource := search(apitypes.SearchRequest{Source: "e2e-src-alpha"})
		if !contains(bySource, e1.Entries[0].Entry.ID) || contains(bySource, e2.Entries[0].Entry.ID) || !contains(bySource, e3.Entries[0].Entry.ID) {
			t.Errorf("wrong source filter: %v (expected %s and %s, not %s)", bySource, e1.Entries[0].Entry.ID, e3.Entries[0].Entry.ID, e2.Entries[0].Entry.ID)
		}

		byAgent := search(apitypes.SearchRequest{Agent: "filter-agent"})
		if !contains(byAgent, e2.Entries[0].Entry.ID) || contains(byAgent, e1.Entries[0].Entry.ID) || contains(byAgent, e3.Entries[0].Entry.ID) {
			t.Errorf("wrong agent filter: %v (expected only %s)", byAgent, e2.Entries[0].Entry.ID)
		}
	})

	t.Run("tag post-filter excludes entries without the tag", func(t *testing.T) {
		domain := "code:filter-tags"
		add := func(content string, tags []string) apitypes.AddResponse {
			resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{
				Content: content, Domain: domain, Tags: tags,
			})
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("add tag: status %d, body %s", resp.StatusCode, data)
			}
			var out apitypes.AddResponse
			mustUnmarshal(t, data, &out)
			return out
		}
		eGolang := add("Peregrine falcon bird of prey speed record dive", []string{"golang", "backend"})
		eOther := add("Peregrine falcon bird of prey speed record hunt", []string{"python"})

		resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/search", srv.APIKey, apitypes.SearchRequest{
			Query:  "peregrine falcon bird of prey speed record",
			Domain: domain,
			Limit:  20,
			Tags:   []string{"golang"},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("search tag: status %d, body %s", resp.StatusCode, data)
		}
		var out apitypes.SearchResponse
		mustUnmarshal(t, data, &out)

		var gotGolang, gotOutro bool
		for _, r := range out.Results {
			if r.ID == eGolang.Entries[0].Entry.ID {
				gotGolang = true
			}
			if r.ID == eOther.Entries[0].Entry.ID {
				gotOutro = true
			}
		}
		if !gotGolang {
			t.Error("expected the entry with the golang tag in the results")
		}
		if gotOutro {
			t.Error("entry without the golang tag leaked into the filtered result")
		}
	})

	t.Run("secret redaction before indexing", func(t *testing.T) {
		secret := "sk-" + "a1B2c3D4a1B2c3D4a1B2c3D4"
		domain := "code:test-e2e-redact"
		content := "my openai key is " + secret + " please do not leak this anywhere"

		resp, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/memories", srv.APIKey, apitypes.AddRequest{
			Content: content,
			Domain:  domain,
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("add: status %d, body %s", resp.StatusCode, data)
		}
		var added apitypes.AddResponse
		mustUnmarshal(t, data, &added)
		if strings.Contains(added.Entries[0].Entry.Content, secret) {
			t.Fatal("secret leaked in the add response itself")
		}
		if !strings.Contains(added.Entries[0].Entry.Content, "[REDACTED]") {
			t.Errorf("expected [REDACTED] in the content returned by add, got %q", added.Entries[0].Entry.Content)
		}

		recentResp, data := doJSON(t, http.MethodGet, srv.URL+"/api/v1/recent?domain="+domain, srv.APIKey, nil)
		if recentResp.StatusCode != http.StatusOK {
			t.Fatalf("recent: status %d, body %s", recentResp.StatusCode, data)
		}
		var recent apitypes.RecentResponse
		mustUnmarshal(t, data, &recent)
		var checked bool
		for _, e := range recent.Results {
			if e.ID != added.Entries[0].Entry.ID {
				continue
			}
			checked = true
			if strings.Contains(e.Content, secret) {
				t.Fatal("secret leaked in the stored content (via /recent)")
			}
			if !strings.Contains(e.Content, "[REDACTED]") {
				t.Errorf("expected [REDACTED] in the persisted content, got %q", e.Content)
			}
		}
		if !checked {
			t.Fatal("memory with secret did not appear in /recent")
		}
	})
}

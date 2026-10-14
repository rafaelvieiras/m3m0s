package redact

import (
	"strings"
	"testing"
)

// All values below are synthetic.
const fakePw = "Zx9-fake-password-1"

func TestRedactKeyValueVariants(t *testing.T) {
	cases := []struct{ name, in string }{
		{"underscore prefix", "TEST_USER_PASSWORD=" + fakePw},
		{"admin password", "ADMIN_PASSWORD=" + fakePw},
		{"access token", "access_token=" + fakePw},
		{"client secret", "CLIENT_SECRET=" + fakePw},
		{"refresh token", "refresh_token: " + fakePw},
		{"api key secret", "API_KEY_SECRET=" + fakePw},
		{"camel case", "clientSecret = " + fakePw},
		{"pwd", "DB_PWD=" + fakePw},
		{"passwd", "passwd=" + fakePw},
		{"double quoted", `PASSWORD="` + fakePw + `"`},
		{"single quoted", `password: '` + fakePw + `'`},
		{"quoted with spaces", `password = "two words here"`},
		{"json", `{"db_password": "` + fakePw + `"}`},
		{"short low entropy", "ADMIN_PASSWORD=admin1"},
		{"short word", "password=abc"},
		{"mixed case key", "Api_Key=" + fakePw},
		{"cli flag", "run --password " + fakePw + " now"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Redact(c.in)
			assertNoValue(t, r.Text, c.in)
			if len(r.Redacted) == 0 {
				t.Fatalf("no rule reported for %q", c.in)
			}
		})
	}
}

func TestRedactKeyValueKeepsTheKeyName(t *testing.T) {
	r := Redact("TEST_USER_PASSWORD=" + fakePw)
	if r.Text != "TEST_USER_PASSWORD="+placeholder {
		t.Fatalf("expected the key name to survive, got %q", r.Text)
	}
}

func TestRedactNarrativeSecrets(t *testing.T) {
	cases := []string{
		"the Basic Auth user is 'someuser' and the password is '" + fakePw + "'",
		"the password is " + fakePw,
		"Password is '" + fakePw + "'.",
		"the admin password for staging is 'pw1'x",
		"the admin user password is “" + fakePw + "”",
		"the api key is `" + fakePw + "`",
		"the password is hunter2",
		"the password was 'abc'",
	}
	for _, in := range cases {
		r := Redact(in)
		if strings.Contains(r.Text, "fake-password") || strings.Contains(r.Text, "hunter2") ||
			strings.Contains(r.Text, "'abc'") || strings.Contains(r.Text, "pw1") {
			t.Fatalf("value leaked in %q -> %q", in, r.Text)
		}
		if len(r.Redacted) == 0 {
			t.Fatalf("no rule reported for %q", in)
		}
	}
}

func TestRedactAuthorizationHeaders(t *testing.T) {
	cases := []struct{ in, scheme string }{
		{"Authorization: Basic dXNlcjpGYWtlUHcxMjM=", "Basic"},
		{"authorization: bearer " + fakePw, "bearer"},
		{`-H "Authorization: Bearer ` + fakePw + `"`, "Bearer"},
		{"Authorization: Token " + fakePw, "Token"},
	}
	for _, c := range cases {
		r := Redact(c.in)
		if strings.Contains(r.Text, fakePw) || strings.Contains(r.Text, "dXNlcjpGYWtlUHcxMjM=") {
			t.Fatalf("credential leaked: %q", r.Text)
		}
		if !strings.Contains(r.Text, c.scheme) {
			t.Fatalf("scheme %q should survive: %q", c.scheme, r.Text)
		}
	}
}

func TestRedactURLUserinfo(t *testing.T) {
	cases := []string{
		"https://someuser:" + fakePw + "@example.com/path",
		"git clone http://ci:Zx9fakepw1@host.example:8080/repo.git",
		"ftp://someuser:abc@files.example",
	}
	for _, in := range cases {
		r := Redact(in)
		if strings.Contains(r.Text, "Zx9") || strings.Contains(r.Text, ":abc@") {
			t.Fatalf("userinfo password leaked: %q", r.Text)
		}
		if !strings.Contains(r.Text, "@") {
			t.Fatalf("host part should survive: %q", r.Text)
		}
	}
	if got := Redact("https://example.com/a@b"); got.Text != "https://example.com/a@b" {
		t.Fatalf("URL without userinfo changed: %q", got.Text)
	}
}

func TestRedactProviderTokens(t *testing.T) {
	for _, in := range []string{
		"xoxb-123456789012-abcdefghijklmnop",
		"AIza" + strings.Repeat("a1B2c3D4", 4) + "xyz",
		"github_pat_" + strings.Repeat("aB3dE5gH", 4),
		"sk-proj-" + strings.Repeat("aB3dE5gH_", 4),
	} {
		if r := Redact("value " + in + " end"); strings.Contains(r.Text, in) {
			t.Fatalf("provider token leaked: %q", r.Text)
		}
	}
}

func TestRedactKeepsPlaceholdersAndDescriptions(t *testing.T) {
	cases := []string{
		"the callback carries #access_token=... in the URL fragment",
		"set DB_PASSWORD=${DB_PASSWORD} in the compose file",
		"export API_KEY=$MY_API_KEY",
		"password=<your-password>",
		"token: string",
		"password: required",
		"max_tokens=1024 and token_type=bearer",
		"the password reset is done in the admin panel",
		"a secret is sent to the server",
		"the password is required for all accounts",
		"password = getPassword()",
		"Authorization: Bearer <token>",
		"Authorization: Bearer $TOKEN",
		"postgres-style URLs look like https://user:<password>@host",
		"token := loadToken()",
		"DB_PASSWORD=YOUR_DB_PASSWORD",
	}
	for _, in := range cases {
		if r := Redact(in); r.Text != in {
			t.Errorf("false positive:\n in:  %q\n out: %q (%v)", in, r.Text, r.Redacted)
		}
	}
}

func TestRedactKeepsIdentifiers(t *testing.T) {
	cases := []string{
		"the function buildSessionSummaryFromTranscript builds the summary",
		"branch feat/some-long-branch-name-for-the-fix is open",
		"see internal/store/vecindex.go and internal/sqlstore/migrations/00006_insights.sql",
		"migrations/00006_memory_health_dashboard_insights.sql",
		"file analise-esquecimento-d22-20261008.md was updated",
		"call extractFactsFromSourceContentChunks() twice",
		"snake_case_identifier_that_is_quite_long_indeed is a variable",
		"worktree m3m0s-oss-redact on fix/redactor-secrets-and-scan-tool",
		"briefs/BF19-brief-reestruturacao-ux-admin.md",
		"TestRedactHighEntropyIdentifiersAreKept_Subtest",
	}
	for _, in := range cases {
		if r := Redact(in); r.Text != in {
			t.Errorf("identifier redacted:\n in:  %q\n out: %q (%v)", in, r.Text, r.Redacted)
		}
	}
}

func TestRedactStillCatchesRandomTokens(t *testing.T) {
	for _, tok := range []string{
		"aB3fK9zQmR7xT2vL8pN4wY",
		"sk_test_4eC39HqLyjWDarjtT1zdp7dc",
		"Zx9aB3fK9zQmR7xT2vL8pN4wYqW5eR",
		"kJhGfDsAqWeRtYuIoPlMnBvCxZ",
		"3f2b8c1e9d7a4b6c8e0f1a2b3c4d5e6f7a8b9c0d",
	} {
		if r := Redact("value " + tok + " end"); strings.Contains(r.Text, tok) {
			t.Errorf("random token leaked: %q", r.Text)
		}
	}
}

func TestScanReportsRuleIDsWithoutValues(t *testing.T) {
	rules := Scan("ADMIN_PASSWORD=" + fakePw)
	if len(rules) != 1 || rules[0] != "kv-secret" {
		t.Fatalf("Scan = %v", rules)
	}
	if got := Scan("nothing to see"); len(got) != 0 {
		t.Fatalf("Scan = %v", got)
	}
}

func assertNoValue(t *testing.T, out, in string) {
	t.Helper()
	if out == in {
		t.Fatalf("not redacted: %q", in)
	}
	for _, leak := range []string{"fake-password", "Zx9", "admin1", "two words", "=abc"} {
		if strings.Contains(out, leak) {
			t.Fatalf("value leaked (%q) in %q -> %q", leak, in, out)
		}
	}
}

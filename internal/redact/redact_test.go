package redact

import "testing"

func TestRedactSpecificPatterns(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"openai-key", "my key is sk-abcdefghijklmnopqrstuvwx1234"},
		{"aws-access-key-id", "AKIAABCDEFGHIJKLMNOP in the code"},
		{"github-token", "token: ghp_abcdefghijklmnopqrstuvwxyz012345"},
		{"jwt", "authorization: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dGVzdHNpZ25hdHVyZQ"},
		{"connection-string", "postgres://user:pass@host:5432/db"},
		{"kv-secret", `password="supersecret123"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := Redact(c.in)
			if result.Text == c.in {
				t.Fatalf("Redact(%q) did not change the text", c.in)
			}
			if !containsPlaceholder(result.Text) {
				t.Fatalf("Redact(%q) = %q, no placeholder", c.in, result.Text)
			}
		})
	}
}

func TestRedactHighEntropyToken(t *testing.T) {
	in := "loose secret in the text: aB3fK9zQmR7xT2vL8pN4wY with no known prefix"
	result := Redact(in)
	if !containsPlaceholder(result.Text) {
		t.Fatalf("high-entropy token should be redacted: %q", result.Text)
	}
}

func TestRedactDoesNotTouchOrdinaryText(t *testing.T) {
	in := "this is just ordinary text, with no secret anywhere inside it."
	result := Redact(in)
	if result.Text != in {
		t.Fatalf("ordinary text should not be changed: %q", result.Text)
	}
	if len(result.Redacted) != 0 {
		t.Fatalf("should not have redacted anything: %v", result.Redacted)
	}
}

func TestRedactDoesNotFlagLongCommonWords(t *testing.T) {
	// An ordinary URL, with no credentials, must not trigger the
	// high-entropy heuristic (avoids false positives on normal documentation text).
	in := "see the documentation at https://docs.example.com/guide/how-to-configure-the-complete-environment"
	result := Redact(in)
	if result.Text != in {
		t.Fatalf("ordinary URL should not be redacted: %q", result.Text)
	}
}

func containsPlaceholder(s string) bool {
	for i := 0; i+len(placeholder) <= len(s); i++ {
		if s[i:i+len(placeholder)] == placeholder {
			return true
		}
	}
	return false
}

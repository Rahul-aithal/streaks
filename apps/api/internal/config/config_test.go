package config

import "testing"

func TestParseLine(t *testing.T) {
	tests := []struct {
		line, key, value string
		ok               bool
	}{
		{"", "", "", false},
		{"# comment", "", "", false},
		{"not a pair", "", "", false},
		{"A=b", "A", "b", true},
		{"export A=b", "A", "b", true},
		{`A="b c"`, "A", "b c", true},
		{"A='b'", "A", "b", true},
		{
			"DATABASE_URL=postgresql://u:p@h/db?sslmode=require&channel_binding=require",
			"DATABASE_URL", "postgresql://u:p@h/db?sslmode=require&channel_binding=require", true,
		},
	}
	for _, tc := range tests {
		key, value, ok := parseLine(tc.line)
		if key != tc.key || value != tc.value || ok != tc.ok {
			t.Errorf("parseLine(%q) = %q, %q, %v", tc.line, key, value, ok)
		}
	}
}

func TestParseEmails(t *testing.T) {
	got := parseEmails(" A@Example.com, b@example.com ,,")
	if len(got) != 2 || got[0] != "a@example.com" || got[1] != "b@example.com" {
		t.Errorf("parseEmails = %v", got)
	}
	if got := parseEmails(""); len(got) != 0 {
		t.Errorf("empty list = %v", got)
	}
}

func TestValidateServeRequiresAllowlist(t *testing.T) {
	c := Config{DatabaseURL: "x", GoogleClientID: "x", GoogleClientSecret: "x", GoogleTokenKey: "x"}
	if err := c.ValidateServe(); err == nil {
		t.Error("an empty allowlist must not start the server")
	}
	c.AllowedEmails = []string{"a@example.com"}
	if err := c.ValidateServe(); err != nil {
		t.Error(err)
	}
}

func TestWebOrigin(t *testing.T) {
	c := Config{GoogleRedirectURI: "http://localhost:5173/api/oauth/google/callback"}
	if got := c.WebOrigin(); got != "http://localhost:5173" {
		t.Errorf("WebOrigin = %q", got)
	}
	if c.SecureCookies() {
		t.Error("http should not use secure cookies")
	}
}

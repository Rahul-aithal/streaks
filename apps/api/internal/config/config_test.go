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

func TestValidateServe(t *testing.T) {
	c := Config{DatabaseURL: "x", GoogleClientID: "x", GoogleClientSecret: "x"}
	if err := c.ValidateServe(); err == nil {
		t.Error("a missing token key must not start the server")
	}
	c.GoogleTokenKey = "x"
	if err := c.ValidateServe(); err != nil {
		t.Error(err)
	}
	c.GitHubClientID = "x"
	if err := c.ValidateServe(); err == nil {
		t.Error("a GitHub client ID without its secret must not start the server")
	}
	c.GitHubClientSecret = "x"
	if err := c.ValidateServe(); err != nil {
		t.Error(err)
	}
	c.SMTPHost = "smtp.example.com"
	if err := c.ValidateServe(); err == nil {
		t.Error("SMTP without a from address must not start the server")
	}
	c.EmailFrom = "resend" // Resend's SMTP username, which isn't an address.
	if err := c.ValidateServe(); err == nil {
		t.Error("a from value that isn't an address must not start the server")
	}
	c.EmailFrom = "Streaks <me@example.com>"
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

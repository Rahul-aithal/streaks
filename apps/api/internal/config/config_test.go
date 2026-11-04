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

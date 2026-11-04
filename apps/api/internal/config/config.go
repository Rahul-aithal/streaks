// Package config reads settings from the environment and an optional .env file.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL       string
	DatabaseURLDirect string
	APIPort           string
	SyncInterval      time.Duration

	// Only these Google accounts may sign in. Lowercased.
	AllowedEmails []string

	GoogleClientID     string
	GoogleClientSecret string
	GoogleTokenKey     string
	GoogleRedirectURI  string
}

// SecureCookies reports whether the app is served over HTTPS.
func (c Config) SecureCookies() bool { return strings.HasPrefix(c.GoogleRedirectURI, "https://") }

// WebOrigin is the origin the browser app is served from, taken from the redirect URI.
func (c Config) WebOrigin() string {
	u, err := url.Parse(c.GoogleRedirectURI)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// ValidateServe reports the first setting the API can't run without.
func (c Config) ValidateServe() error {
	switch {
	case c.DatabaseURL == "":
		return errors.New("DATABASE_URL is required (copy .env.example to .env)")
	case c.GoogleClientID == "" || c.GoogleClientSecret == "":
		return errors.New("GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET are required: sign-in uses Google")
	case c.GoogleTokenKey == "":
		return errors.New("GOOGLE_TOKEN_KEY is required: openssl rand -hex 32")
	case len(c.AllowedEmails) == 0:
		return errors.New("ALLOWED_EMAILS is required: list the Google accounts that may sign in")
	}
	return nil
}

// Load reads .env (real environment variables win) and returns the config.
// Connection strings are parsed here rather than sourced by a shell because
// Neon URLs contain "&", which a shell would misread.
func Load() (Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return Config{}, err
	}
	c := Config{
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		DatabaseURLDirect:  os.Getenv("DATABASE_URL_DIRECT"),
		APIPort:            envOr("API_PORT", "8080"),
		AllowedEmails:      parseEmails(os.Getenv("ALLOWED_EMAILS")),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleTokenKey:     os.Getenv("GOOGLE_TOKEN_KEY"),
		GoogleRedirectURI:  envOr("GOOGLE_REDIRECT_URI", "http://localhost:5173/api/oauth/google/callback"),
	}
	interval, err := time.ParseDuration(envOr("SYNC_INTERVAL", "6h"))
	if err != nil || interval <= 0 {
		return Config{}, fmt.Errorf("invalid SYNC_INTERVAL %q", os.Getenv("SYNC_INTERVAL"))
	}
	c.SyncInterval = interval
	return c, nil
}

func parseEmails(list string) []string {
	emails := []string{}
	for _, e := range strings.Split(list, ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			emails = append(emails, e)
		}
	}
	return emails
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := parseLine(scanner.Text())
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func parseLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	key, value, ok = strings.Cut(strings.TrimPrefix(line, "export "), "=")
	if !ok {
		return "", "", false
	}
	key, value = strings.TrimSpace(key), strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	return key, value, key != ""
}

// Package config reads settings from the environment and an optional .env file.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL       string
	DatabaseURLDirect string
	APISecret         string
	APIPort           string
	SyncInterval      time.Duration

	GitHubUsername string
	GitHubToken    string

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRefreshToken string
}

// GitHubEnabled reports whether GitHub sync is configured.
func (c Config) GitHubEnabled() bool { return c.GitHubUsername != "" && c.GitHubToken != "" }

// GoogleEnabled reports whether Google Tasks sync is configured.
func (c Config) GoogleEnabled() bool {
	return c.GoogleClientID != "" && c.GoogleClientSecret != "" && c.GoogleRefreshToken != ""
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
		APISecret:          os.Getenv("API_SECRET"),
		APIPort:            envOr("API_PORT", "8080"),
		GitHubUsername:     os.Getenv("GITHUB_USERNAME"),
		GitHubToken:        os.Getenv("GITHUB_TOKEN"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRefreshToken: os.Getenv("GOOGLE_REFRESH_TOKEN"),
	}
	interval, err := time.ParseDuration(envOr("SYNC_INTERVAL", "6h"))
	if err != nil || interval <= 0 {
		return Config{}, fmt.Errorf("invalid SYNC_INTERVAL %q", os.Getenv("SYNC_INTERVAL"))
	}
	c.SyncInterval = interval
	return c, nil
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

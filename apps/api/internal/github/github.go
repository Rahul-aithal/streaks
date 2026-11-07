// Package github reads a user's contribution calendar: the green graph on their
// profile.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"streaks/internal/store"
	"streaks/internal/streak"
)

// DefaultBaseURL is the public API.
const DefaultBaseURL = "https://api.github.com"

// historyDays is how far back each sync reads. GitHub allows at most a year per
// query, and re-reading it all keeps the counts in step with GitHub's graph.
const historyDays = 364

// ErrBadToken means GitHub rejected the token.
var ErrBadToken = errors.New("GitHub rejected this token")

// Client acts for one user with their token.
type Client struct {
	Token   string
	BaseURL string
	HTTP    *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{Token: token, BaseURL: baseURL, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// Login returns the username the token belongs to, so nobody can link a token
// to the wrong account by mistyping a name.
func (c *Client) Login(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/user", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("github user: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", ErrBadToken
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github user: status %d", resp.StatusCode)
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil || user.Login == "" {
		return "", errors.New("github user: unexpected response")
	}
	return user.Login, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return c.HTTP.Do(req)
}

const calendarQuery = `query($login: String!, $from: DateTime!, $to: DateTime!) {
  user(login: $login) {
    contributionsCollection(from: $from, to: $to) {
      contributionCalendar { weeks { contributionDays { date contributionCount } } }
    }
  }
}`

// Contributions returns the user's daily contribution counts for the past year,
// the same numbers as their profile graph: commits, pull requests, issues and
// reviews. Private work is included as a count when their profile is set to
// show private contributions. Only days with contributions are returned; from
// and to are the range read, so days missing from it had none.
func (c *Client) Contributions(ctx context.Context, login string, now time.Time) (days []store.GitHubDay, from, to streak.Day, err error) {
	start := now.AddDate(0, 0, -historyDays)
	body, err := json.Marshal(map[string]any{
		"query": calendarQuery,
		"variables": map[string]string{
			"login": login, "from": start.UTC().Format(time.RFC3339), "to": now.UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return nil, 0, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("github contributions: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, 0, 0, ErrBadToken
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, 0, 0, fmt.Errorf("github contributions: status %d: %s", resp.StatusCode, msg)
	}

	var out struct {
		Data struct {
			User *struct {
				Collection struct {
					Calendar struct {
						Weeks []struct {
							Days []struct {
								Date  string `json:"date"`
								Count int    `json:"contributionCount"`
							} `json:"contributionDays"`
						} `json:"weeks"`
					} `json:"contributionCalendar"`
				} `json:"contributionsCollection"`
			} `json:"user"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, 0, fmt.Errorf("github contributions: decode: %w", err)
	}
	if len(out.Errors) > 0 {
		return nil, 0, 0, fmt.Errorf("github contributions: %s", out.Errors[0].Message)
	}
	if out.Data.User == nil {
		return nil, 0, 0, fmt.Errorf("github contributions: user %q not found", login)
	}
	for _, w := range out.Data.User.Collection.Calendar.Weeks {
		for _, d := range w.Days {
			if d.Count <= 0 {
				continue
			}
			day, err := streak.ParseDay(d.Date)
			if err != nil {
				return nil, 0, 0, fmt.Errorf("github contributions: %w", err)
			}
			days = append(days, store.GitHubDay{Day: day, Count: d.Count})
		}
	}
	return days, streak.DayOf(start), streak.DayOf(now), nil
}

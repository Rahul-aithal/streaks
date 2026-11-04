// Package github reads the user's recent activity from the REST Events API.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"streaks/internal/store"
)

const (
	perPage  = 100
	maxPages = 3 // The API serves at most 300 events from the past 30 days.
)

// DefaultBaseURL is the public API.
const DefaultBaseURL = "https://api.github.com"

// ErrBadToken means GitHub rejected the token.
var ErrBadToken = errors.New("GitHub rejected this token")

// Client acts for one user with their personal access token.
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
	resp, err := c.do(ctx, c.BaseURL+"/user")
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

func (c *Client) do(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return c.HTTP.Do(req)
}

type rawEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Payload   struct {
		Action string `json:"action"`
	} `json:"payload"`
}

// counts reports whether an event is activity: a push or an opened PR. PR
// updates, comments, stars and so on are not.
func (e rawEvent) counts() bool {
	switch e.Type {
	case "PushEvent":
		return true
	case "PullRequestEvent":
		return e.Payload.Action == "opened"
	}
	return false
}

// FetchActivity returns all counted events the API still retains.
func (c *Client) FetchActivity(ctx context.Context, login string) ([]store.GitHubEvent, error) {
	var out []store.GitHubEvent
	for page := 1; page <= maxPages; page++ {
		events, done, err := c.fetchPage(ctx, login, page)
		if err != nil {
			return nil, err
		}
		for _, e := range events {
			if e.counts() {
				out = append(out, store.GitHubEvent{ID: e.ID, Type: e.Type, OccurredAt: e.CreatedAt})
			}
		}
		if done {
			break
		}
	}
	return out, nil
}

func (c *Client) fetchPage(ctx context.Context, login string, page int) (events []rawEvent, done bool, err error) {
	u := fmt.Sprintf("%s/users/%s/events?per_page=%d&page=%d", c.BaseURL, url.PathEscape(login), perPage, page)
	resp, err := c.do(ctx, u)
	if err != nil {
		return nil, false, fmt.Errorf("github events: %w", err)
	}
	defer resp.Body.Close()

	// Paging past the retained window is an end of data, not a failure.
	if resp.StatusCode == http.StatusUnprocessableEntity && page > 1 {
		return nil, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, false, fmt.Errorf("github events: status %d: %s", resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, false, fmt.Errorf("github events: decode: %w", err)
	}
	return events, len(events) < perPage, nil
}

// Package github reads the user's recent activity from the REST Events API.
package github

import (
	"context"
	"encoding/json"
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

type Client struct {
	Username string
	Token    string
	BaseURL  string // Defaults to the public API; overridden in tests.
	HTTP     *http.Client
}

func NewClient(username, token string) *Client {
	return &Client{
		Username: username,
		Token:    token,
		BaseURL:  "https://api.github.com",
		HTTP:     &http.Client{Timeout: 20 * time.Second},
	}
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
func (c *Client) FetchActivity(ctx context.Context) ([]store.GitHubEvent, error) {
	var out []store.GitHubEvent
	for page := 1; page <= maxPages; page++ {
		events, done, err := c.fetchPage(ctx, page)
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

func (c *Client) fetchPage(ctx context.Context, page int) (events []rawEvent, done bool, err error) {
	u := fmt.Sprintf("%s/users/%s/events?per_page=%d&page=%d", c.BaseURL, url.PathEscape(c.Username), perPage, page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.HTTP.Do(req)
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

// Package gtasks reads tasks from the Google Tasks API, read-only.
package gtasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"streaks/internal/store"
	"streaks/internal/streak"
)

// Scope is the only permission requested.
const Scope = "https://www.googleapis.com/auth/tasks.readonly"

// TokenURL is Google's OAuth token endpoint.
const TokenURL = "https://oauth2.googleapis.com/token"

// completedRetention limits how much old completed work is cached.
const completedRetention = 7

type Client struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	BaseURL      string
	TokenURL     string
	HTTP         *http.Client
}

func NewClient(clientID, clientSecret, refreshToken string) *Client {
	return &Client{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RefreshToken: refreshToken,
		BaseURL:      "https://tasks.googleapis.com/tasks/v1",
		TokenURL:     TokenURL,
		HTTP:         &http.Client{Timeout: 20 * time.Second},
	}
}

type rawTask struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Due       string     `json:"due"`
	Completed *time.Time `json:"completed"`
}

// FetchTasks returns open tasks and recently completed tasks from all lists.
func (c *Client) FetchTasks(ctx context.Context, today streak.Day) ([]store.Task, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	lists, err := c.listIDs(ctx, token)
	if err != nil {
		return nil, err
	}
	var out []store.Task
	for _, list := range lists {
		pageToken := ""
		for {
			q := url.Values{
				"maxResults":    {"100"},
				"showCompleted": {"true"},
				"showHidden":    {"true"}, // Needed for tasks completed in Google's own apps.
			}
			if pageToken != "" {
				q.Set("pageToken", pageToken)
			}
			var page struct {
				Items         []rawTask `json:"items"`
				NextPageToken string    `json:"nextPageToken"`
			}
			if err := c.get(ctx, token, "/lists/"+url.PathEscape(list)+"/tasks?"+q.Encode(), &page); err != nil {
				return nil, err
			}
			for _, r := range page.Items {
				if t, keep := convert(r, list, today); keep {
					out = append(out, t)
				}
			}
			if page.NextPageToken == "" {
				break
			}
			pageToken = page.NextPageToken
		}
	}
	return out, nil
}

func convert(r rawTask, list string, today streak.Day) (store.Task, bool) {
	t := store.Task{GoogleID: r.ID, ListID: list, Title: r.Title, Status: r.Status, CompletedAt: r.Completed}
	if t.Status == "completed" && r.Completed != nil && streak.DayOf(*r.Completed) < today-completedRetention {
		return t, false
	}
	// Google stores only the date, as midnight UTC. Take the date as written:
	// converting to IST could only move it incorrectly.
	if len(r.Due) >= 10 {
		if d, err := streak.ParseDay(r.Due[:10]); err == nil {
			t.Due = &d
		}
	}
	return t, true
}

// ForToday returns the tasks that belong on today's view: open tasks due today
// or earlier, plus dated tasks completed today. Undated tasks are left out.
func ForToday(tasks []store.Task, today streak.Day) ([]store.Task, store.TaskSnapshot) {
	items := []store.Task{}
	var snap store.TaskSnapshot
	for _, t := range tasks {
		if t.Due == nil {
			continue
		}
		if t.Status == "completed" {
			if t.CompletedAt != nil && streak.DayOf(*t.CompletedAt) == today {
				snap.Completed++
				items = append(items, t)
			}
			continue
		}
		if *t.Due > today {
			continue
		}
		if *t.Due < today {
			snap.Overdue++
		}
		items = append(items, t)
	}
	snap.Total = len(items)
	return items, snap
}

func (c *Client) listIDs(ctx context.Context, token string) ([]string, error) {
	var ids []string
	pageToken := ""
	for {
		q := url.Values{"maxResults": {"100"}}
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.get(ctx, token, "/users/@me/lists?"+q.Encode(), &page); err != nil {
			return nil, err
		}
		for _, l := range page.Items {
			ids = append(ids, l.ID)
		}
		if page.NextPageToken == "" {
			return ids, nil
		}
		pageToken = page.NextPageToken
	}
}

func (c *Client) get(ctx context.Context, token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("google tasks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("google tasks: status %d: %s", resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("google tasks: decode: %w", err)
	}
	return nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	tok, err := ExchangeToken(ctx, c.HTTP, c.TokenURL, url.Values{
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"refresh_token": {c.RefreshToken},
		"grant_type":    {"refresh_token"},
	})
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// Token is Google's token endpoint response.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// ExchangeToken posts a grant to the token endpoint. Errors include Google's
// error text but never the request, which holds the credentials.
func ExchangeToken(ctx context.Context, hc *http.Client, tokenURL string, form url.Values) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return Token{}, fmt.Errorf("google token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&e)
		msg := fmt.Sprintf("google token: status %d: %s %s", resp.StatusCode, e.Error, e.Description)
		if e.Error == "invalid_grant" {
			msg += " (refresh token expired or revoked: run `make google-auth`; the OAuth app must be In production)"
		}
		return Token{}, errors.New(msg)
	}
	var tok Token
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return Token{}, fmt.Errorf("google token: decode: %w", err)
	}
	return tok, nil
}

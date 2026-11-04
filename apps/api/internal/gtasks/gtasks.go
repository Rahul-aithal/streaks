// Package gtasks reads tasks from the Google Tasks API, read-only.
package gtasks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"streaks/internal/store"
	"streaks/internal/streak"
)

// TasksScope is the only permission on the user's data. openid and email just
// identify which account connected.
const TasksScope = "https://www.googleapis.com/auth/tasks.readonly"

// ErrReconnect means Google no longer accepts the account's refresh token.
var ErrReconnect = errors.New("Google access expired or was revoked: reconnect this account")

// ErrMissingScope means the user unticked Tasks access on Google's consent screen.
var ErrMissingScope = errors.New("Google Tasks access was not granted")

// ErrNoRefreshToken means Google didn't issue a long-lived token.
var ErrNoRefreshToken = errors.New("Google returned no refresh token")

// completedRetention limits how much old completed work is cached.
const completedRetention = 7

// Client holds the app's OAuth credentials. It's shared by all accounts; each
// call takes the refresh token of the account it acts for.
type Client struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	AuthURL      string
	TokenURL     string
	RevokeURL    string
	BaseURL      string
	HTTP         *http.Client
}

func NewClient(clientID, clientSecret, redirectURI string) *Client {
	return &Client{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURI,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		RevokeURL:    "https://oauth2.googleapis.com/revoke",
		BaseURL:      "https://tasks.googleapis.com/tasks/v1",
		HTTP:         &http.Client{Timeout: 20 * time.Second},
	}
}

// AuthCodeURL returns the page where the user picks an account and approves access.
func (c *Client) AuthCodeURL(state string) string {
	return c.AuthURL + "?" + url.Values{
		"client_id":     {c.ClientID},
		"redirect_uri":  {c.RedirectURI},
		"response_type": {"code"},
		"scope":         {"openid email " + TasksScope},
		"access_type":   {"offline"},
		// select_account lets you add a second account while signed in to the
		// first; consent makes Google issue a refresh token every time.
		"prompt": {"select_account consent"},
		"state":  {state},
	}.Encode()
}

// Identity is a newly connected account.
type Identity struct {
	Sub          string
	Email        string
	RefreshToken string
}

// ExchangeCode trades the code from the OAuth callback for the account's identity and token.
func (c *Client) ExchangeCode(ctx context.Context, code string) (Identity, error) {
	tok, err := c.postToken(ctx, url.Values{
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"code":          {code},
		"redirect_uri":  {c.RedirectURI},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		return Identity{}, err
	}
	if !slices.Contains(strings.Fields(tok.Scope), TasksScope) {
		return Identity{}, ErrMissingScope
	}
	if tok.RefreshToken == "" {
		return Identity{}, ErrNoRefreshToken
	}
	sub, email, err := parseIDToken(tok.IDToken)
	if err != nil {
		return Identity{}, err
	}
	return Identity{Sub: sub, Email: email, RefreshToken: tok.RefreshToken}, nil
}

// parseIDToken reads the claims of an ID token. Its signature needn't be checked
// because the token came directly from Google over TLS, not through the browser.
func parseIDToken(token string) (sub, email string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", errors.New("google token: malformed ID token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", errors.New("google token: malformed ID token")
	}
	var claims struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil || claims.Sub == "" {
		return "", "", errors.New("google token: ID token has no account ID")
	}
	return claims.Sub, claims.Email, nil
}

// Revoke tells Google to invalidate a refresh token.
func (c *Client) Revoke(ctx context.Context, refreshToken string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.RevokeURL,
		strings.NewReader(url.Values{"token": {refreshToken}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return errors.New("google revoke: request failed") // The URL error could quote nothing useful.
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("google revoke: status %d", resp.StatusCode)
	}
	return nil
}

type rawTask struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Due       string     `json:"due"`
	Completed *time.Time `json:"completed"`
}

// FetchTasks returns one account's open and recently completed tasks from all its lists.
func (c *Client) FetchTasks(ctx context.Context, refreshToken string, today streak.Day) ([]store.Task, error) {
	token, err := c.accessToken(ctx, refreshToken)
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

func (c *Client) accessToken(ctx context.Context, refreshToken string) (string, error) {
	tok, err := c.postToken(ctx, url.Values{
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	})
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
}

// postToken posts a grant to the token endpoint. Errors include Google's error
// text but never the request, which holds the credentials.
func (c *Client) postToken(ctx context.Context, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return tokenResponse{}, fmt.Errorf("google token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&e)
		if e.Error == "invalid_grant" {
			return tokenResponse{}, ErrReconnect
		}
		return tokenResponse{}, fmt.Errorf("google token: status %d: %s %s", resp.StatusCode, e.Error, e.Description)
	}
	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return tokenResponse{}, fmt.Errorf("google token: decode: %w", err)
	}
	return tok, nil
}

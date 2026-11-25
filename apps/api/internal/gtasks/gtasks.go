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

// TasksScope is the only permission on the user's data: read tasks, and mark
// them done or open again. openid and email just identify which account connected.
const TasksScope = "https://www.googleapis.com/auth/tasks"

// ReadOnlyScope is the old, read-only permission. A token with only this one
// still syncs, but can't tick tasks off until the account reconnects.
const ReadOnlyScope = "https://www.googleapis.com/auth/tasks.readonly"

// ErrReconnect means Google no longer accepts the account's refresh token.
var ErrReconnect = errors.New("Google access expired or was revoked: reconnect this account")

// ErrNeedsWriteAccess means the account signed in when Streaks could only read
// tasks. Signing in again grants the current scope.
var ErrNeedsWriteAccess = errors.New("Streaks can only read your Google Tasks: reconnect Google to let it mark tasks done")

// ErrTaskGone means the task no longer exists in Google Tasks.
var ErrTaskGone = errors.New("this task no longer exists in Google Tasks")

// errUnauthorizedClient means the grant belongs to a different OAuth client.
var errUnauthorizedClient = errors.New("google token: unauthorized_client")

// ErrMissingScope means the user unticked Tasks access on Google's consent screen.
var ErrMissingScope = errors.New("Google Tasks access was not granted")

// ErrUnverifiedEmail means Google hasn't verified the account's email address.
var ErrUnverifiedEmail = errors.New("Google account email is not verified")

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

// AuthCodeURL returns the page where the user picks an account. With forceConsent,
// Google also shows the permissions again and issues a new refresh token, which
// a returning user doesn't otherwise get or need.
func (c *Client) AuthCodeURL(state string, forceConsent bool) string {
	prompt := "select_account"
	if forceConsent {
		prompt = "select_account consent"
	}
	return c.AuthURL + "?" + url.Values{
		"client_id":     {c.ClientID},
		"redirect_uri":  {c.RedirectURI},
		"response_type": {"code"},
		"scope":         {"openid email profile " + TasksScope},
		"access_type":   {"offline"},
		"prompt":        {prompt}, // select_account allows adding a second account.
		"state":         {state},
	}.Encode()
}

// Identity is the account that just signed in.
type Identity struct {
	Sub          string
	Email        string
	Name         string
	AvatarURL    string
	RefreshToken string // Empty for a returning user unless consent was forced.
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
	if scopes := strings.Fields(tok.Scope); !slices.Contains(scopes, TasksScope) && !slices.Contains(scopes, ReadOnlyScope) {
		return Identity{}, ErrMissingScope
	}
	identity, err := parseIDToken(tok.IDToken)
	if err != nil {
		return Identity{}, err
	}
	identity.RefreshToken = tok.RefreshToken
	return identity, nil
}

// parseIDToken reads the claims of an ID token. Its signature needn't be checked
// because the token came directly from Google over TLS, not through the browser.
func parseIDToken(token string) (Identity, error) {
	malformed := errors.New("google token: malformed ID token")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Identity{}, malformed
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Identity{}, malformed
	}
	var claims struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil || claims.Sub == "" || claims.Email == "" {
		return Identity{}, errors.New("google token: ID token has no account details")
	}
	if !claims.EmailVerified {
		return Identity{}, ErrUnverifiedEmail
	}
	return Identity{Sub: claims.Sub, Email: claims.Email, Name: claims.Name, AvatarURL: claims.Picture}, nil
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
		return errors.New("google revoke: request failed")
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

// FetchTasks returns one account's open and recently completed tasks from all
// its lists, and whether its token may also change them.
func (c *Client) FetchTasks(ctx context.Context, refreshToken string, today streak.Day) ([]store.Task, bool, error) {
	tok, err := c.refresh(ctx, refreshToken)
	if err != nil {
		return nil, false, err
	}
	// Google lists the token's scopes. If it doesn't, don't nag about access.
	writable := tok.Scope == "" || slices.Contains(strings.Fields(tok.Scope), TasksScope)
	token := tok.AccessToken
	lists, err := c.listIDs(ctx, token)
	if err != nil {
		return nil, false, err
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
				return nil, false, err
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
	return out, writable, nil
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

// Later returns the tasks that aren't on today's view: open tasks with no date
// or a future one, plus undated tasks completed today so finishing one shows.
// Dated tasks come first, soonest first, then undated ones in input order.
func Later(tasks []store.Task, today streak.Day) []store.Task {
	dated, undated := []store.Task{}, []store.Task{}
	for _, t := range tasks {
		switch {
		case t.Status == "completed":
			if t.Due == nil && t.CompletedAt != nil && streak.DayOf(*t.CompletedAt) == today {
				undated = append(undated, t)
			}
		case t.Due == nil:
			undated = append(undated, t)
		case *t.Due > today:
			dated = append(dated, t)
		}
	}
	slices.SortStableFunc(dated, func(a, b store.Task) int { return int(*a.Due - *b.Due) })
	return append(dated, undated...)
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

// SetTaskStatus marks a task completed, or open again, and returns its new state.
func (c *Client) SetTaskStatus(ctx context.Context, refreshToken, listID, taskID string, done bool) (store.Task, error) {
	token, err := c.accessToken(ctx, refreshToken)
	if err != nil {
		return store.Task{}, err
	}
	// Reopening must clear the completion time too, or Google keeps it.
	body := `{"status":"needsAction","completed":null}`
	if done {
		body = `{"status":"completed"}`
	}
	var r rawTask
	path := "/lists/" + url.PathEscape(listID) + "/tasks/" + url.PathEscape(taskID)
	if err := c.send(ctx, http.MethodPatch, token, path, strings.NewReader(body), &r); err != nil {
		return store.Task{}, err
	}
	t := store.Task{GoogleID: r.ID, ListID: listID, Title: r.Title, Status: r.Status, CompletedAt: r.Completed}
	if len(r.Due) >= 10 {
		if d, err := streak.ParseDay(r.Due[:10]); err == nil {
			t.Due = &d
		}
	}
	return t, nil
}

func (c *Client) get(ctx context.Context, token, path string, out any) error {
	return c.send(ctx, http.MethodGet, token, path, nil, out)
}

func (c *Client) send(ctx context.Context, method, token, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("google tasks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		switch {
		case resp.StatusCode == http.StatusForbidden && strings.Contains(string(msg), "SCOPE_INSUFFICIENT"),
			resp.StatusCode == http.StatusForbidden && strings.Contains(string(msg), "insufficient"):
			return ErrNeedsWriteAccess
		case resp.StatusCode == http.StatusNotFound && method != http.MethodGet:
			return ErrTaskGone
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("google tasks: status %d: %s", resp.StatusCode, msg)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("google tasks: decode: %w", err)
	}
	return nil
}

func (c *Client) accessToken(ctx context.Context, refreshToken string) (string, error) {
	tok, err := c.refresh(ctx, refreshToken)
	return tok.AccessToken, err
}

// refresh trades a refresh token for an access token and its scopes.
func (c *Client) refresh(ctx context.Context, refreshToken string) (tokenResponse, error) {
	tok, err := c.postToken(ctx, url.Values{
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	})
	// The refresh token was issued to another OAuth client, usually because
	// GOOGLE_CLIENT_ID changed since sign-in. Signing in again issues one for
	// the current client.
	if errors.Is(err, errUnauthorizedClient) {
		return tokenResponse{}, ErrReconnect
	}
	return tok, err
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
		switch e.Error {
		case "invalid_grant":
			return tokenResponse{}, ErrReconnect
		case "unauthorized_client":
			return tokenResponse{}, fmt.Errorf("%w: %s", errUnauthorizedClient, e.Description)
		}
		return tokenResponse{}, fmt.Errorf("google token: status %d: %s %s", resp.StatusCode, e.Error, e.Description)
	}
	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return tokenResponse{}, fmt.Errorf("google token: decode: %w", err)
	}
	return tok, nil
}

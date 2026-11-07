package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultWebURL hosts GitHub's OAuth pages, which aren't on the API host.
const DefaultWebURL = "https://github.com"

// OAuth is the "Connect GitHub" flow for an OAuth app. It requests no scopes, so
// the token can read public data only and can't change anything.
type OAuth struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	WebURL       string
	APIURL       string
	HTTP         *http.Client
}

func NewOAuth(clientID, clientSecret, redirectURI string) *OAuth {
	return &OAuth{
		ClientID: clientID, ClientSecret: clientSecret, RedirectURI: redirectURI,
		WebURL: DefaultWebURL, APIURL: DefaultBaseURL,
		HTTP: &http.Client{Timeout: 20 * time.Second},
	}
}

// AuthorizeURL is where the browser goes to approve the connection.
func (o *OAuth) AuthorizeURL(state string) string {
	return o.WebURL + "/login/oauth/authorize?" + url.Values{
		"client_id":    {o.ClientID},
		"redirect_uri": {o.RedirectURI},
		"state":        {state},
		// Lets someone signed in to several GitHub accounts choose which to connect.
		"prompt": {"select_account"},
	}.Encode()
}

// Exchange swaps the callback's code for an access token.
func (o *OAuth) Exchange(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"client_id":     {o.ClientID},
		"client_secret": {o.ClientSecret},
		"code":          {code},
		"redirect_uri":  {o.RedirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.WebURL+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("github token: %w", err)
	}
	defer resp.Body.Close()

	// GitHub reports a bad code with status 200 and an error field.
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("github token: status %d: decode: %w", resp.StatusCode, err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("github token: status %d: %s", resp.StatusCode, out.Error)
	}
	return out.AccessToken, nil
}

// Revoke invalidates a token this app issued. A pasted personal token, or one
// that's already gone, returns 404 and is treated as nothing to do.
func (o *OAuth) Revoke(ctx context.Context, token string) error {
	body, err := json.Marshal(map[string]string{"access_token": token})
	if err != nil {
		return err
	}
	u := o.APIURL + "/applications/" + url.PathEscape(o.ClientID) + "/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(o.ClientID, o.ClientSecret)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("github revoke: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return errors.New("github revoke: status " + resp.Status)
	}
	return nil
}

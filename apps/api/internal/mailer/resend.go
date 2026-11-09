package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

// ResendURL is Resend's send-email endpoint.
const ResendURL = "https://api.resend.com/emails"

// Resend sends through Resend's HTTPS API. Unlike SMTP it only needs port 443,
// which free hosts don't block.
type Resend struct {
	APIKey string
	From   string
	URL    string       // Defaults to ResendURL. Tests point it elsewhere.
	HTTP   *http.Client // Defaults to a client with a 30 s timeout.
}

func (r *Resend) Send(ctx context.Context, m Message) error {
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return fmt.Errorf("recipient: %w", err)
	}
	from, err := mail.ParseAddress(r.From)
	if err != nil {
		return fmt.Errorf("EMAIL_FROM: %w", err)
	}
	if strings.ContainsAny(m.Subject, "\r\n") {
		return errors.New("subject contains a line break")
	}
	body, err := json.Marshal(map[string]any{
		"from":    (&mail.Address{Name: "Streaks", Address: from.Address}).String(),
		"to":      []string{to.Address},
		"subject": m.Subject,
		"text":    m.Text,
		"html":    m.HTML,
	})
	if err != nil {
		return err
	}

	url := r.URL
	if url == "" {
		url = ResendURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "streaks")

	client := r.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		return nil
	}
	// Errors look like {"name": "...", "message": "..."}; the key never appears.
	var e struct {
		Message string `json:"message"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if json.Unmarshal(raw, &e) == nil && e.Message != "" {
		return fmt.Errorf("resend: %s (HTTP %d)", e.Message, resp.StatusCode)
	}
	return fmt.Errorf("resend: HTTP %d", resp.StatusCode)
}

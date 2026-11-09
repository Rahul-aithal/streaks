// Package mailer sends email over SMTP or Resend's HTTPS API, using only the standard library.
package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Message is one email with plain-text and HTML versions.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// SMTP sends through a mail server. Port 465 uses TLS from the start; any other
// port must offer STARTTLS, except on localhost, so passwords never go out in
// the clear.
type SMTP struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
	Timeout  time.Duration
}

func (s *SMTP) Send(ctx context.Context, m Message) error {
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return fmt.Errorf("recipient: %w", err)
	}
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("EMAIL_FROM: %w", err)
	}
	if strings.ContainsAny(m.Subject, "\r\n") {
		return errors.New("subject contains a line break")
	}
	body, err := build(from.Address, to.Address, m)
	if err != nil {
		return err
	}

	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addr := net.JoinHostPort(s.Host, s.Port)
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline) // The smtp package has no timeouts of its own.
	if s.Port == "465" {
		conn = tls.Client(conn, &tls.Config{ServerName: s.Host})
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp: %w", err)
	}
	defer c.Close()

	if s.Port != "465" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		} else if !isLocal(s.Host) {
			return errors.New("the mail server doesn't offer STARTTLS, so the password would be sent unencrypted")
		}
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("smtp login: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp sender: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("smtp recipient: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	return c.Quit()
}

func isLocal(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func build(from, to string, m Message) ([]byte, error) {
	boundary, err := randomHex(12)
	if err != nil {
		return nil, err
	}
	id, err := randomHex(12)
	if err != nil {
		return nil, err
	}
	domain := "streaks.local"
	if at := strings.LastIndex(from, "@"); at >= 0 {
		domain = from[at+1:]
	}

	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", (&mail.Address{Name: "Streaks", Address: from}).String())
	header("To", (&mail.Address{Address: to}).String())
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("Message-ID", "<"+id+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	for _, part := range []struct{ kind, body string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		fmt.Fprintf(&b, "--%s\r\n", boundary)
		header("Content-Type", part.kind+"; charset=utf-8")
		header("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		qp := quotedprintable.NewWriter(&b)
		if _, err := qp.Write([]byte(strings.ReplaceAll(part.body, "\n", "\r\n"))); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), nil
}

func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

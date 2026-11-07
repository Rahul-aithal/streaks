package mailer

import (
	"bufio"
	"context"
	"io"
	"mime/quotedprintable"
	"net"
	"strings"
	"testing"
)

// fakeSMTP accepts one message on localhost and returns what it received.
func fakeSMTP(t *testing.T) (port string, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		say := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		say("220 fake")
		var log strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			log.WriteString(line)
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				say("250-fake")
				say("250 AUTH PLAIN")
			case strings.HasPrefix(cmd, "AUTH"):
				say("235 ok")
			case cmd == "DATA":
				say("354 go")
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					log.WriteString(l)
				}
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				got <- log.String()
				return
			default:
				say("250 ok")
			}
		}
	}()
	_, port, _ = net.SplitHostPort(ln.Addr().String())
	return port, got
}

func TestSend(t *testing.T) {
	port, got := fakeSMTP(t)
	s := &SMTP{Host: "127.0.0.1", Port: port, Username: "me@example.com", Password: "pw", From: "me@example.com"}
	err := s.Send(context.Background(), Message{
		To: "you@example.com", Subject: "Streaks: 2 habits — today", Text: "Hello\nworld", HTML: "<p>Hello</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := <-got
	for _, want := range []string{
		"AUTH PLAIN", "MAIL FROM:<me@example.com>", "RCPT TO:<you@example.com>",
		`From: "Streaks" <me@example.com>`, "Subject: =?utf-8?q?", "multipart/alternative",
		"text/plain; charset=utf-8", "text/html; charset=utf-8",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing %q in:\n%s", want, raw)
		}
	}
	decoded, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(raw[strings.Index(raw, "Hello"):])))
	if !strings.Contains(string(decoded), "Hello\r\nworld") {
		t.Errorf("body lines should end in CRLF: %q", decoded)
	}
}

func TestSendRejectsHeaderInjection(t *testing.T) {
	s := &SMTP{Host: "127.0.0.1", Port: "1", From: "me@example.com"}
	if err := s.Send(context.Background(), Message{To: "you@example.com", Subject: "hi\r\nBcc: x@example.com"}); err == nil {
		t.Error("subject with a line break accepted")
	}
	if err := s.Send(context.Background(), Message{To: "you@example.com\r\nBcc: x@example.com", Subject: "hi"}); err == nil {
		t.Error("recipient with a line break accepted")
	}
}

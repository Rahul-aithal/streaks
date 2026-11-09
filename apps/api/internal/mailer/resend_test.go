package mailer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResendSend(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer re_key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer srv.Close()

	r := &Resend{APIKey: "re_key", From: "me@example.com", URL: srv.URL}
	err := r.Send(context.Background(), Message{To: "you@example.com", Subject: "Hi", Text: "t", HTML: "<p>h</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if got["from"] != `"Streaks" <me@example.com>` || got["subject"] != "Hi" || got["text"] != "t" || got["html"] != "<p>h</p>" {
		t.Errorf("body = %v", got)
	}
	if to, _ := got["to"].([]any); len(to) != 1 || to[0] != "you@example.com" {
		t.Errorf("to = %v", got["to"])
	}
}

func TestResendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"name":"validation_error","message":"domain is not verified"}`))
	}))
	defer srv.Close()

	r := &Resend{APIKey: "k", From: "me@example.com", URL: srv.URL}
	err := r.Send(context.Background(), Message{To: "you@example.com", Subject: "Hi"})
	if err == nil || !strings.Contains(err.Error(), "domain is not verified") {
		t.Errorf("err = %v", err)
	}
	err = r.Send(context.Background(), Message{To: "you@example.com", Subject: "a\r\nBcc: x@example.com"})
	if err == nil {
		t.Error("a subject with a line break must be refused")
	}
}

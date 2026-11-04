// Command streaks is the API server, migration runner, and Google auth helper.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"streaks/internal/config"
	"streaks/internal/github"
	"streaks/internal/gtasks"
	"streaks/internal/httpapi"
	"streaks/internal/store"
	"streaks/internal/syncer"
	"streaks/migrations"
)

const usage = `usage: streaks <command>

  serve            run the API
  migrate up       apply migrations (uses DATABASE_URL_DIRECT)
  migrate down     roll back the latest migration
  migrate status   show migration state
  google-auth      get a Google refresh token for .env`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	switch args[0] {
	case "serve":
		return serve(cfg)
	case "migrate":
		return migrate(cfg, args[1:])
	case "google-auth":
		return googleAuth(cfg)
	}
	return errors.New(usage)
}

func serve(cfg config.Config) error {
	if cfg.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required (copy .env.example to .env)")
	}
	if cfg.APISecret == "" || cfg.APISecret == "change-me" {
		return errors.New("set API_SECRET to a real value: openssl rand -hex 32")
	}
	// Soft cap that keeps the heap small. Set GOMEMLIMIT to override.
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(40 << 20)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	sync := &syncer.Syncer{Store: st, Interval: cfg.SyncInterval, Now: time.Now}
	if cfg.GitHubEnabled() {
		sync.GitHub = github.NewClient(cfg.GitHubUsername, cfg.GitHubToken)
	} else {
		slog.Info("GitHub sync disabled: set GITHUB_USERNAME and GITHUB_TOKEN")
	}
	if cfg.GoogleEnabled() {
		sync.Google = gtasks.NewClient(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRefreshToken)
	} else {
		slog.Info("Google Tasks sync disabled: set the GOOGLE_* variables")
	}
	go sync.Run(ctx)

	api := &httpapi.Server{Store: st, Sync: sync, Secret: cfg.APISecret, Now: time.Now}
	srv := &http.Server{
		// Loopback only: this is a personal tool reached through the web dev server.
		Addr:              "127.0.0.1:" + cfg.APIPort,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      3 * time.Minute, // A manual sync can take a while.
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("API listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func migrate(cfg config.Config, args []string) error {
	if len(args) != 1 {
		return errors.New(usage)
	}
	if cfg.DatabaseURLDirect == "" {
		return errors.New("DATABASE_URL_DIRECT is required for migrations")
	}
	db, err := sql.Open("pgx", cfg.DatabaseURLDirect)
	if err != nil {
		return errors.New("invalid DATABASE_URL_DIRECT")
	}
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	switch args[0] {
	case "up":
		results, err := provider.Up(ctx)
		if err != nil {
			return err
		}
		for _, r := range results {
			fmt.Println("applied", r.Source.Path)
		}
		if len(results) == 0 {
			fmt.Println("already up to date")
		}
	case "down":
		r, err := provider.Down(ctx)
		if err != nil {
			return err
		}
		fmt.Println("rolled back", r.Source.Path)
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return err
		}
		for _, s := range statuses {
			fmt.Printf("%-10s %s\n", s.State, s.Source.Path)
		}
	default:
		return errors.New(usage)
	}
	return nil
}

// googleAuth runs the OAuth installed-app flow on a loopback port and prints a
// refresh token. It needs a "Desktop app" OAuth client.
func googleAuth(cfg config.Config) error {
	if cfg.GoogleClientID == "" || cfg.GoogleClientSecret == "" {
		return errors.New("set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET in .env first")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d/", ln.Addr().(*net.TCPAddr).Port)

	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	state := hex.EncodeToString(raw)

	authURL := "https://accounts.google.com/o/oauth2/v2/auth?" + url.Values{
		"client_id":     {cfg.GoogleClientID},
		"redirect_uri":  {redirect},
		"response_type": {"code"},
		"scope":         {gtasks.Scope},
		"access_type":   {"offline"},
		"prompt":        {"consent"}, // Google only returns a refresh token on consent.
		"state":         {state},
	}.Encode()
	fmt.Println("Open this URL and approve access:\n\n" + authURL + "\n")
	_ = exec.Command("xdg-open", authURL).Start() // Best effort; the URL is printed anyway.

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if e := q.Get("error"); e != "" {
			fmt.Fprintln(w, "Authorization failed:", e)
			results <- result{err: errors.New(e)}
			return
		}
		fmt.Fprintln(w, "Done. You can close this tab and go back to the terminal.")
		results <- result{code: q.Get("code")}
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var res result
	select {
	case res = <-results:
	case <-ctx.Done():
		return errors.New("timed out waiting for Google approval")
	}
	if res.err != nil {
		return res.err
	}
	tok, err := gtasks.ExchangeToken(ctx, &http.Client{Timeout: 20 * time.Second}, gtasks.TokenURL, url.Values{
		"client_id":     {cfg.GoogleClientID},
		"client_secret": {cfg.GoogleClientSecret},
		"code":          {res.code},
		"redirect_uri":  {redirect},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(tok.RefreshToken) == "" {
		return errors.New("Google returned no refresh token; revoke the app's access at myaccount.google.com/permissions and retry")
	}
	fmt.Println("Add this line to .env:\n\nGOOGLE_REFRESH_TOKEN=" + tok.RefreshToken)
	return nil
}

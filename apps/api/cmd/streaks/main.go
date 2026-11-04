// Command streaks is the API server, migration runner, and Google auth helper.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"streaks/internal/config"
	"streaks/internal/github"
	"streaks/internal/gtasks"
	"streaks/internal/httpapi"
	"streaks/internal/secretbox"
	"streaks/internal/store"
	"streaks/internal/syncer"
	"streaks/migrations"
)

const usage = `usage: streaks <command>

  serve            run the API
  migrate up       apply migrations (uses DATABASE_URL_DIRECT)
  migrate down     roll back the latest migration
  migrate status   show migration state`

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
	}
	return errors.New(usage)
}

func serve(cfg config.Config) error {
	if err := cfg.ValidateServe(); err != nil {
		return err
	}
	box, err := secretbox.New(cfg.GoogleTokenKey)
	if err != nil {
		return fmt.Errorf("GOOGLE_TOKEN_KEY: %w", err)
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

	sync := &syncer.Syncer{
		Store:         st,
		Google:        gtasks.NewClient(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURI),
		Box:           box,
		GitHubBaseURL: github.DefaultBaseURL,
		Interval:      cfg.SyncInterval,
		Now:           time.Now,
	}
	go sync.Run(ctx)

	api := &httpapi.Server{
		Store:         st,
		Sync:          sync,
		Now:           time.Now,
		WebOrigin:     cfg.WebOrigin(),
		SecureCookies: cfg.SecureCookies(),
	}
	srv := &http.Server{
		// Loopback only: it's reached through the web dev server.
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

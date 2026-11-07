package reminder

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"streaks/internal/mailer"
	"streaks/internal/store"
	"streaks/internal/streak"
)

type fakeMail struct {
	mu   sync.Mutex
	sent []mailer.Message
	fail bool
}

func (f *fakeMail) Send(_ context.Context, m mailer.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("server down")
	}
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeMail) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// Set TEST_DATABASE_URL to a disposable, migrated database to run this test. It
// deletes rows, so never point it at real data.
func TestScheduledSends(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for _, table := range []string{"users", "habits"} { // Cascades to everything else.
		if _, err := conn.Exec(ctx, "DELETE FROM "+table); err != nil {
			t.Fatal(err)
		}
	}

	alice, _ := st.SaveSignIn(ctx, "sub-a", "alice@example.com", "Alice", "", "x")
	bob, _ := st.SaveSignIn(ctx, "sub-b", "bob@example.com", "Bob", "", "x")
	carol, _ := st.SaveSignIn(ctx, "sub-c", "carol@example.com", "Carol", "", "x") // No habits.
	today := day("2026-11-04")
	for _, u := range []int64{alice, bob} {
		if _, err := st.CreateHabit(ctx, u, "read", "manual", streak.EveryDay, today); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetEmailReminders(ctx, bob, false); err != nil {
		t.Fatal(err)
	}

	clock := time.Date(2026, 11, 4, 10, 0, 0, 0, streak.IST)
	mail := &fakeMail{}
	r := &Reminder{Store: st, Mail: mail, Now: func() time.Time { return clock }, next: map[string]time.Time{}}

	r.tick(ctx)
	if mail.count() != 0 {
		t.Fatal("sent before 11:00")
	}

	// 11:00: only alice. Bob turned emails off, carol has no habits.
	clock = clock.Add(time.Hour)
	r.tick(ctx)
	if mail.count() != 1 || mail.sent[0].To != "alice@example.com" {
		t.Fatalf("sent = %+v", mail.sent)
	}
	clock = clock.Add(5 * time.Minute)
	r.tick(ctx)
	if mail.count() != 1 {
		t.Error("sent twice in one window")
	}

	// A restart forgets memory, but the database remembers.
	r.next = map[string]time.Time{}
	r.tick(ctx)
	if mail.count() != 1 {
		t.Error("sent again after a restart")
	}

	// 17:00 fails, then is retried 10 minutes later.
	clock = time.Date(2026, 11, 4, 17, 0, 0, 0, streak.IST)
	mail.fail = true
	r.tick(ctx)
	mail.fail = false
	clock = clock.Add(5 * time.Minute)
	r.tick(ctx)
	if mail.count() != 1 {
		t.Error("retried too soon")
	}
	clock = clock.Add(6 * time.Minute)
	r.tick(ctx)
	if mail.count() != 2 || mail.sent[1].Subject != "Streaks: 0 of 1 habit done today" {
		t.Errorf("retry: %+v", mail.sent)
	}

	// Laptop asleep through the whole 22:00 window: nothing late and stale.
	clock = time.Date(2026, 11, 4, 23, 30, 0, 0, streak.IST)
	r.tick(ctx)
	if mail.count() != 2 {
		t.Error("sent after the window closed")
	}

	// A test email works any time and doesn't use up the scheduled one.
	u, _ := st.GetUser(ctx, alice)
	if err := r.SendTest(ctx, u); err != nil {
		t.Fatal(err)
	}
	if got := mail.sent[2].Subject; got != "[Test] Streaks: 1 habit left today" {
		t.Errorf("test subject = %q", got)
	}
	c, _ := st.GetUser(ctx, carol)
	if err := r.SendTest(ctx, c); !errors.Is(err, ErrNoHabits) {
		t.Errorf("test with no habits: %v", err)
	}
}

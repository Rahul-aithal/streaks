package store

import (
	"context"
	"os"
	"testing"
	"time"

	"streaks/internal/streak"
)

// Set TEST_DATABASE_URL to a disposable, migrated database to run this test. It
// deletes rows, so never point it at real data.
func TestSyncRoundTrip(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, table := range []string{"habits", "github_events", "google_accounts", "task_snapshots", "sync_runs"} {
		if _, err := s.pool.Exec(ctx, "DELETE FROM "+table); err != nil {
			t.Fatal(err)
		}
	}

	today, _ := streak.ParseDay("2026-11-04")
	id, err := s.CreateHabit(ctx, "code", "github", streak.EveryDay, today)
	if err != nil {
		t.Fatal(err)
	}
	// 18:30Z on the 3rd is 00:00 IST on the 4th.
	at, _ := time.Parse(time.RFC3339, "2026-11-03T18:30:00Z")
	events := []GitHubEvent{
		{ID: "1", Type: "PushEvent", OccurredAt: at},
		{ID: "2", Type: "PushEvent", OccurredAt: at.Add(-time.Second)},
	}
	for range 2 { // Re-running must not double-count.
		if err := s.SaveGitHubEvents(ctx, events); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := s.ContributionCounts(ctx, today-1, today)
	if err != nil {
		t.Fatal(err)
	}
	if counts[today] != 1 || counts[today-1] != 1 {
		t.Errorf("counts = %v", counts)
	}
	h, err := s.GetHabit(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.CheckIns) != 2 || h.CheckIns[0] != today-1 || h.CheckIns[1] != today {
		t.Errorf("check-ins = %v", h.CheckIns)
	}
	if got := streak.Compute(streak.Habit{Schedules: h.Schedules, CheckIns: h.CheckIns}, today); got.Current != 2 {
		t.Errorf("streak = %+v", got)
	}

	// A habit created later picks up activity that's already synced.
	later, err := s.CreateHabit(ctx, "code 2", "github", streak.EveryDay, today)
	if err != nil {
		t.Fatal(err)
	}
	if h, _ := s.GetHabit(ctx, later); len(h.CheckIns) != 2 {
		t.Errorf("later habit check-ins = %v", h.CheckIns)
	}

	if err := s.SaveGitHubEvents(ctx, nil); err != nil {
		t.Errorf("empty sync: %v", err)
	}

	due := today
	done := at
	tasks := []Task{
		{GoogleID: "a", ListID: "L", Title: "dated", Status: "needsAction", Due: &due},
		{GoogleID: "b", ListID: "L", Title: "undated", Status: "needsAction"},
		{GoogleID: "c", ListID: "L", Title: "done", Status: "completed", Due: &due, CompletedAt: &done},
	}
	account, err := s.UpsertGoogleAccount(ctx, "sub-1", "a@example.com", "sealed-1")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.UpsertGoogleAccount(ctx, "sub-2", "b@example.com", "sealed-2")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.SaveAccountTasks(ctx, account, tasks); err != nil {
			t.Fatal(err)
		}
	}
	// The same task ID under another account must not collide.
	if err := s.SaveAccountTasks(ctx, other, tasks[:1]); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTaskSnapshot(ctx, today, TaskSnapshot{Total: 2, Completed: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTaskSnapshot(ctx, today, TaskSnapshot{Total: 3}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("tasks = %+v", got)
	}
	for _, task := range got {
		if task.AccountEmail != "a@example.com" {
			continue
		}
		switch task.GoogleID {
		case "a":
			if task.Due == nil || *task.Due != today || task.CompletedAt != nil {
				t.Errorf("task a = %+v", task)
			}
		case "b":
			if task.Due != nil {
				t.Errorf("task b = %+v", task)
			}
		case "c":
			if task.CompletedAt == nil || !task.CompletedAt.Equal(done) {
				t.Errorf("task c = %+v", task)
			}
		}
	}
	if err := s.SaveAccountTasks(ctx, account, nil); err != nil {
		t.Errorf("empty task sync: %v", err)
	}
	if got, _ := s.ListTasks(ctx); len(got) != 1 || got[0].AccountEmail != "b@example.com" {
		t.Errorf("clearing one account should leave the other: %+v", got)
	}
	if err := s.DeleteGoogleAccount(ctx, other); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListTasks(ctx); len(got) != 0 {
		t.Errorf("deleting an account should remove its tasks: %+v", got)
	}

	// Sync state: a failure keeps the last success time.
	if err := s.RecordSync(ctx, "x", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSync(ctx, "x", context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	run, err := s.GetSyncRun(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	if run.LastSuccessAt == nil || run.LastError == nil {
		t.Errorf("run = %+v", run)
	}
}

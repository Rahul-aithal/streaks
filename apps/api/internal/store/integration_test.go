package store

import (
	"context"
	"errors"
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
	for _, table := range []string{"users", "habits"} { // Cascades to everything else.
		if _, err := s.pool.Exec(ctx, "DELETE FROM "+table); err != nil {
			t.Fatal(err)
		}
	}
	user, err := s.SaveSignIn(ctx, "sub-1", "a@example.com", "A", "", "sealed-1")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.SaveSignIn(ctx, "sub-2", "b@example.com", "B", "", "sealed-2")
	if err != nil {
		t.Fatal(err)
	}

	today, _ := streak.ParseDay("2026-11-04")
	id, err := s.CreateHabit(ctx, user, "code", "github", streak.EveryDay, today)
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
		if err := s.SaveGitHubEvents(ctx, user, events); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := s.ContributionCounts(ctx, user, today-1, today)
	if err != nil {
		t.Fatal(err)
	}
	if counts[today] != 1 || counts[today-1] != 1 {
		t.Errorf("counts = %v", counts)
	}
	h, err := s.GetHabit(ctx, user, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.CheckIns) != 2 || h.CheckIns[0] != today-1 || h.CheckIns[1] != today {
		t.Errorf("check-ins = %v", h.CheckIns)
	}
	if got := streak.Compute(streak.Habit{Schedules: h.Schedules, CheckIns: h.CheckIns}, today); got.Current != 2 {
		t.Errorf("streak = %+v", got)
	}

	// Only one active GitHub habit, but any number of manual ones.
	if _, err := s.CreateHabit(ctx, user, "code 2", "github", streak.EveryDay, today); !errors.Is(err, ErrGitHubHabitExists) {
		t.Fatalf("second GitHub habit: %v", err)
	}
	for _, name := range []string{"read", "gym"} {
		if _, err := s.CreateHabit(ctx, user, name, "manual", streak.EveryDay, today); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateHabit(ctx, other, "their code", "github", streak.EveryDay, today); err != nil {
		t.Errorf("another user's GitHub habit: %v", err)
	}

	// A habit created later picks up activity that's already synced.
	if err := s.ArchiveHabit(ctx, user, id, today); err != nil {
		t.Fatal(err)
	}
	later, err := s.CreateHabit(ctx, user, "code 2", "github", streak.EveryDay, today)
	if err != nil {
		t.Fatal(err)
	}
	if h, _ := s.GetHabit(ctx, user, later); len(h.CheckIns) != 2 {
		t.Errorf("later habit check-ins = %v", h.CheckIns)
	}
	if err := s.UnarchiveHabit(ctx, user, id, today); !errors.Is(err, ErrGitHubHabitExists) {
		t.Errorf("unarchiving a second GitHub habit: %v", err)
	}

	if err := s.SaveGitHubEvents(ctx, user, nil); err != nil {
		t.Errorf("empty sync: %v", err)
	}

	due := today
	done := at
	tasks := []Task{
		{GoogleID: "a", ListID: "L", Title: "dated", Status: "needsAction", Due: &due},
		{GoogleID: "b", ListID: "L", Title: "undated", Status: "needsAction"},
		{GoogleID: "c", ListID: "L", Title: "done", Status: "completed", Due: &due, CompletedAt: &done},
	}
	for range 2 {
		if err := s.SaveTasks(ctx, user, tasks, today, TaskSnapshot{Total: 2, Completed: 1}); err != nil {
			t.Fatal(err)
		}
	}
	// The same task ID under another user must not collide.
	if err := s.SaveTasks(ctx, other, tasks[:1], today, TaskSnapshot{Total: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListTasks(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("tasks = %+v", got)
	}
	for _, task := range got {
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
	if err := s.SaveTasks(ctx, user, nil, today, TaskSnapshot{}); err != nil {
		t.Errorf("empty task sync: %v", err)
	}
	if got, _ := s.ListTasks(ctx, other); len(got) != 1 {
		t.Errorf("clearing one user's tasks should leave the other's: %+v", got)
	}

	// Sync state: a failure keeps the last success time.
	if err := s.RecordSync(ctx, user, "x", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSync(ctx, user, "x", context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	run, err := s.GetSyncRun(ctx, user, "x")
	if err != nil {
		t.Fatal(err)
	}
	if run.LastSuccessAt == nil || run.LastError == nil {
		t.Errorf("run = %+v", run)
	}
}

package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"streaks/internal/streak"
)

// Set TEST_DATABASE_URL to a disposable, migrated database to run this test. It
// deletes rows, so never point it at real data.
func TestRewards(t *testing.T) {
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
	for _, table := range []string{"users", "habits"} {
		if _, err := s.pool.Exec(ctx, "DELETE FROM "+table); err != nil {
			t.Fatal(err)
		}
	}
	user, err := s.SaveSignIn(ctx, "sub-r1", "r1@example.com", "R1", "", "sealed-1")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.SaveSignIn(ctx, "sub-r2", "r2@example.com", "R2", "", "sealed-2")
	if err != nil {
		t.Fatal(err)
	}
	today, _ := streak.ParseDay("2026-11-04")
	wallet := func(id int64) Wallet {
		t.Helper()
		w, err := s.GetWallet(ctx, id, today)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}

	// 7 tasks done yesterday hit the cap of 5; 1 done today; 1 still open.
	yesterday, _ := time.Parse(time.RFC3339, "2026-11-03T10:00:00Z")
	now, _ := time.Parse(time.RFC3339, "2026-11-04T10:00:00Z")
	var tasks []Task
	for i := range 7 {
		tasks = append(tasks, Task{GoogleID: fmt.Sprint("y", i), ListID: "L", Title: "old", Status: "completed", CompletedAt: &yesterday})
	}
	tasks = append(tasks,
		Task{GoogleID: "t", ListID: "L", Title: "today", Status: "completed", CompletedAt: &now},
		Task{GoogleID: "open", ListID: "L", Title: "open", Status: "needsAction"})
	for range 2 { // Re-syncing must not earn twice.
		if err := s.SaveTasks(ctx, user, tasks, today, TaskSnapshot{}); err != nil {
			t.Fatal(err)
		}
	}
	if w := wallet(user); w.Earned != 6 || w.Balance != 6 || w.EarnedToday != 1 {
		t.Fatalf("wallet = %+v, want earned 6, today 1", w)
	}
	// A completed task dropping out of Google's window keeps its point.
	if err := s.SaveTasks(ctx, user, nil, today, TaskSnapshot{}); err != nil {
		t.Fatal(err)
	}
	if w := wallet(user); w.Earned != 6 {
		t.Errorf("earned after tasks vanished = %d", w.Earned)
	}
	if w := wallet(other); w.Earned != 0 {
		t.Errorf("other user's wallet = %+v", w)
	}

	// Spending: a cool-off day (3), then a save (2), leaving 1.
	if err := s.SpendCoolOff(ctx, user, today+1); err != nil {
		t.Fatal(err)
	}
	if err := s.SpendCoolOff(ctx, user, today+1); !errors.Is(err, ErrAlreadySpent) {
		t.Errorf("second cool-off on the same day: %v", err)
	}
	habit, err := s.CreateHabit(ctx, user, "read", "manual", streak.EveryDay, today-3)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SpendSave(ctx, user, habit, today-1); err != nil {
		t.Fatal(err)
	}
	if err := s.SpendSave(ctx, other, habit, today-2); !errors.Is(err, ErrNotEnoughPoints) && !errors.Is(err, ErrAlreadySpent) {
		t.Errorf("saving another user's habit: %v", err)
	}
	if w := wallet(user); w.Spent != 5 || w.Balance != 1 {
		t.Errorf("wallet after spending = %+v", w)
	}
	if err := s.SpendCoolOff(ctx, user, today+2); !errors.Is(err, ErrNotEnoughPoints) {
		t.Errorf("overspending: %v", err)
	}

	h, err := s.GetHabit(ctx, user, habit)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Saves) != 1 || h.Saves[0] != today-1 || len(h.CoolOffs) != 1 || h.CoolOffs[0] != today+1 {
		t.Errorf("habit saves=%v cool-offs=%v", h.Saves, h.CoolOffs)
	}
	list, err := s.ListHabits(ctx, user)
	if err != nil || len(list) != 1 || len(list[0].Saves) != 1 || len(list[0].CoolOffs) != 1 {
		t.Errorf("listed habits = %+v, %v", list, err)
	}

	// A future cool-off day can be refunded; today's can't.
	if err := s.RefundCoolOff(ctx, user, today+1, today); err != nil {
		t.Fatal(err)
	}
	if w := wallet(user); w.Balance != 4 {
		t.Errorf("balance after refund = %d", w.Balance)
	}
	if err := s.SpendCoolOff(ctx, user, today); err != nil {
		t.Fatal(err)
	}
	if err := s.RefundCoolOff(ctx, user, today, today); err != nil {
		t.Fatal(err)
	}
	if days, _ := s.CoolOffDays(ctx, user, today-2); len(days) != 1 || days[0] != today {
		t.Errorf("today's cool-off should survive a refund: %v", days)
	}

	// Deleting a saved habit doesn't refund the save.
	if err := s.DeleteHabit(ctx, user, habit); err != nil {
		t.Fatal(err)
	}
	if w := wallet(user); w.Spent != 5 {
		t.Errorf("spent after deleting a saved habit = %d", w.Spent)
	}

	history, err := s.RewardHistory(ctx, user, 50)
	if err != nil {
		t.Fatal(err)
	}
	points := 0
	for _, e := range history {
		points += e.Points
	}
	if len(history) != 10 || points != 1 { // 8 earnings (2 over the cap) + 2 spends.
		t.Errorf("history has %d events totalling %d: %+v", len(history), points, history)
	}
}

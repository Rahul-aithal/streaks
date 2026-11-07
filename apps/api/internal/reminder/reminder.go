// Package reminder emails each user a streak summary at fixed times of day.
package reminder

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"streaks/internal/mailer"
	"streaks/internal/store"
	"streaks/internal/streak"
	"streaks/internal/syncer"
)

const (
	checkEvery = time.Minute
	// Grace is how late a reminder may still go out, e.g. after the laptop wakes.
	// Later than this, it's skipped rather than arriving stale.
	Grace      = time.Hour
	retryAfter = 10 * time.Minute
	sendLimit  = 2 * time.Minute
)

// Sender delivers one email.
type Sender interface {
	Send(ctx context.Context, m mailer.Message) error
}

type Reminder struct {
	Store  *store.Store
	Sync   *syncer.Syncer
	Mail   Sender
	Now    func() time.Time
	AppURL string

	// Per slot: when to look at it again. It's held in memory so the loop only
	// touches the database at reminder times, which lets Neon scale to zero.
	next map[string]time.Time
}

// DueSlot returns the slot whose send window contains now, if any.
func DueSlot(now time.Time) (streak.Day, Slot, bool) {
	local := now.In(streak.IST)
	y, m, d := local.Date()
	for _, slot := range Slots {
		at := time.Date(y, m, d, slot.Hour, 0, 0, 0, streak.IST)
		if !local.Before(at) && local.Before(at.Add(Grace)) {
			return streak.DayOf(now), slot, true
		}
	}
	return 0, Slot{}, false
}

// Run checks the clock every minute until ctx ends. It compares wall-clock time,
// so it still fires after the laptop sleeps through a reminder's start.
func (r *Reminder) Run(ctx context.Context) {
	r.next = map[string]time.Time{}
	ticker := time.NewTicker(checkEvery)
	defer ticker.Stop()
	for {
		r.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Reminder) tick(ctx context.Context) {
	now := r.Now()
	day, slot, ok := DueSlot(now)
	if !ok {
		clear(r.next)
		return
	}
	key := day.String() + "/" + slot.Name
	if next, seen := r.next[key]; seen && now.Before(next) {
		return
	}
	if r.sendSlot(ctx, day, slot) {
		r.next[key] = now.Add(Grace) // Done: stay quiet until the window closes.
	} else {
		r.next[key] = now.Add(retryAfter)
	}
}

// sendSlot emails everyone who wants this reminder and hasn't had it. It
// reports whether every send succeeded.
func (r *Reminder) sendSlot(ctx context.Context, day streak.Day, slot Slot) bool {
	users, err := r.Store.ListUsers(ctx)
	if err != nil {
		slog.Error("list users for reminders", "err", err)
		return false
	}
	ok := true
	for _, u := range users {
		if !u.EmailReminders || ctx.Err() != nil {
			continue
		}
		claimed, err := r.Store.ClaimReminder(ctx, u.ID, day, slot.Name)
		if err != nil {
			slog.Error("claim reminder", "user", u.ID, "err", err)
			ok = false
			continue
		}
		if !claimed {
			continue // Already sent, maybe before a restart.
		}
		if err := r.send(ctx, u, day, slot, ""); err != nil {
			slog.Error("send reminder", "user", u.ID, "slot", slot.Name, "err", err)
			if err := r.Store.ReleaseReminder(ctx, u.ID, day, slot.Name); err != nil {
				slog.Error("release reminder", "user", u.ID, "err", err)
			}
			ok = false
			continue
		}
		slog.Info("reminder sent", "user", u.ID, "slot", slot.Name)
	}
	return ok
}

// ErrNoHabits means there's nothing to put in the email.
var ErrNoHabits = errors.New("no active habits")

func (r *Reminder) send(ctx context.Context, u store.User, day streak.Day, slot Slot, prefix string) error {
	ctx, cancel := context.WithTimeout(ctx, sendLimit)
	defer cancel()
	// GitHub habits are checked in by the sync, so refresh before reporting them.
	if r.Sync != nil {
		if err := r.Sync.Sync(ctx, u.ID, syncer.KindGitHub); err != nil && !errors.Is(err, syncer.ErrNotConfigured) {
			slog.Warn("GitHub sync before reminder", "user", u.ID, "err", err)
		}
	}
	habits, err := r.Store.ListHabits(ctx, u.ID)
	if err != nil {
		return err
	}
	d, ok := Build(habits, day, slot, r.AppURL)
	if !ok {
		if prefix == "" {
			return nil // Nothing to remind about; the claim stops repeat checks.
		}
		return ErrNoHabits
	}
	d.Subject = prefix + d.Subject
	msg, err := d.Message(u.Email)
	if err != nil {
		return err
	}
	return r.Mail.Send(ctx, msg)
}

// SendTest sends the user the reminder for the latest slot that's started today,
// marked as a test, without affecting the scheduled ones.
func (r *Reminder) SendTest(ctx context.Context, u store.User) error {
	now := r.Now()
	slot := Slots[0]
	for _, s := range Slots {
		if now.In(streak.IST).Hour() >= s.Hour {
			slot = s
		}
	}
	return r.send(ctx, u, streak.DayOf(now), slot, "[Test] ")
}

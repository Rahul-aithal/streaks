// Package streak computes streaks from check-in records. It is pure: no
// database, no clock. Callers pass "today" in, so nothing can drift.
package streak

import (
	"slices"
)

// EveryDay is the mask of a habit with no specific target days.
const EveryDay uint8 = 0b1111111

// Schedule says which weekdays are targets from a given day onward.
// Mask bit 0 is Sunday, bit 6 is Saturday.
type Schedule struct {
	From Day
	Mask uint8
}

// Habit is the input for streak calculation.
type Habit struct {
	Schedules []Schedule // Any order.
	CheckIns  []Day      // Any order.
}

// Result is the outcome of a streak calculation.
type Result struct {
	Current       int  `json:"current"`
	Longest       int  `json:"longest"`
	LastCompleted *Day `json:"last_completed"`
}

// MaskFromDays builds a mask from weekday numbers. No days means every day.
func MaskFromDays(days []int) uint8 {
	if len(days) == 0 {
		return EveryDay
	}
	var mask uint8
	for _, d := range days {
		mask |= 1 << d
	}
	return mask
}

// DaysFromMask is the inverse of MaskFromDays.
func DaysFromMask(mask uint8) []int {
	days := []int{}
	for d := range 7 {
		if mask&(1<<d) != 0 {
			days = append(days, d)
		}
	}
	return days
}

// Tracker answers per-day questions about one habit.
type Tracker struct {
	schedules []Schedule
	done      map[Day]struct{}
	start     Day
	hasStart  bool
	last      *Day
}

// NewTracker prepares a habit for lookups. A habit starts on its first schedule
// or its first check-in, whichever is earlier, so a backdated check-in on a
// brand-new habit counts. Days before the first schedule use its mask.
func NewTracker(h Habit, today Day) *Tracker {
	t := &Tracker{
		schedules: slices.Clone(h.Schedules),
		done:      make(map[Day]struct{}, len(h.CheckIns)),
	}
	slices.SortFunc(t.schedules, func(a, b Schedule) int { return int(a.From - b.From) })
	if len(t.schedules) > 0 {
		t.start, t.hasStart = t.schedules[0].From, true
	}
	for _, d := range h.CheckIns {
		if d > today {
			continue // Future check-ins are ignored rather than counted early.
		}
		t.done[d] = struct{}{}
		if !t.hasStart || d < t.start {
			t.start, t.hasStart = d, true
		}
		if t.last == nil || d > *t.last {
			v := d
			t.last = &v
		}
	}
	return t
}

// Scheduled reports whether d is a target day for this habit.
func (t *Tracker) Scheduled(d Day) bool {
	if !t.hasStart || d < t.start || len(t.schedules) == 0 {
		return false
	}
	mask := t.schedules[0].Mask
	for _, s := range t.schedules {
		if s.From > d {
			break
		}
		mask = s.Mask
	}
	return mask&(1<<d.Weekday()) != 0
}

// Done reports whether there is a check-in on d.
func (t *Tracker) Done(d Day) bool {
	_, ok := t.done[d]
	return ok
}

// Compute returns the streaks of a single habit. Only target days count: a
// non-target day neither breaks the streak nor adds to it, even with a
// check-in. An unfinished today doesn't break the streak.
func Compute(h Habit, today Day) Result {
	t := NewTracker(h, today)
	r := walk(t.start, t.hasStart, today, func(d Day) dayState {
		switch {
		case !t.Scheduled(d):
			return dayRest
		case t.Done(d):
			return dayDone
		}
		return dayMissed
	})
	r.LastCompleted = t.last
	return r
}

// ComputeDaily returns the "daily completion" streak: a day counts when every
// habit scheduled that day is done. Days with nothing scheduled are skipped.
func ComputeDaily(habits []Habit, today Day) Result {
	trackers := make([]*Tracker, 0, len(habits))
	var start Day
	hasStart := false
	for _, h := range habits {
		t := NewTracker(h, today)
		trackers = append(trackers, t)
		if t.hasStart && (!hasStart || t.start < start) {
			start, hasStart = t.start, true
		}
	}
	r := walk(start, hasStart, today, func(d Day) dayState {
		state := dayRest
		for _, t := range trackers {
			if !t.Scheduled(d) {
				continue
			}
			if !t.Done(d) {
				return dayMissed
			}
			state = dayDone
		}
		return state
	})
	return r
}

type dayState int

const (
	dayRest dayState = iota
	dayDone
	dayMissed
)

// walk is the one streak loop shared by habits and daily completion.
func walk(start Day, hasStart bool, today Day, state func(Day) dayState) Result {
	var r Result
	if !hasStart {
		return r
	}
	run := 0
	for d := start; d <= today; d++ {
		switch state(d) {
		case dayDone:
			run++
			r.Longest = max(r.Longest, run)
			v := d
			r.LastCompleted = &v
		case dayMissed:
			if d < today { // Today can still be completed.
				run = 0
			}
		}
	}
	r.Current = run
	return r
}

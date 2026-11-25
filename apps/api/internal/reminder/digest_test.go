package reminder

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"streaks/internal/store"
	"streaks/internal/streak"
)

func day(s string) streak.Day {
	d, err := streak.ParseDay(s)
	if err != nil {
		panic(err)
	}
	return d
}

// habit targets the given weekdays (none means every day) since 2026-10-01 and
// is checked in on the given days.
func habit(name string, weekdays []int, checkIns ...string) store.Habit {
	h := store.Habit{Name: name, Schedules: []streak.Schedule{{From: day("2026-10-01"), Mask: streak.MaskFromDays(weekdays)}}}
	for _, c := range checkIns {
		h.CheckIns = append(h.CheckIns, day(c))
	}
	return h
}

func TestBuild(t *testing.T) {
	today := day("2026-11-04") // A Wednesday.
	archived := habit("old", nil)
	now := time.Now()
	archived.ArchivedAt = &now
	weekends := []int{0, 6}

	tests := []struct {
		name    string
		habits  []store.Habit
		slot    int
		subject string
		todo    []string // name:streak:atRisk
		done    int
		rest    int
	}{
		{
			name: "morning, one streak running", slot: 0,
			habits:  []store.Habit{habit("read", nil, "2026-11-02", "2026-11-03"), habit("gym", nil)},
			subject: "2 habits today. Start with the easy one",
			todo:    []string{"read:2:true", "gym:0:false"},
		},
		{
			name: "afternoon counts progress", slot: 1,
			habits:  []store.Habit{habit("read", nil, "2026-11-03", "2026-11-04"), habit("gym", nil)},
			subject: "1 of 2 done. Keep going!",
			todo:    []string{"gym:0:false"}, done: 1,
		},
		{
			name: "night warns about streaks at risk", slot: 2,
			habits:  []store.Habit{habit("read", nil, "2026-11-03"), habit("gym", nil, "2026-11-03")},
			subject: "🔥 2 streaks end at midnight, up to 1 day",
			todo:    []string{"read:1:true", "gym:1:true"},
		},
		{
			name: "night with nothing at risk", slot: 2,
			habits:  []store.Habit{habit("gym", nil)},
			subject: "1 habit left. Finish strong",
			todo:    []string{"gym:0:false"},
		},
		{
			name: "all done", slot: 2,
			habits:  []store.Habit{habit("read", nil, "2026-11-04")},
			subject: "🎉 All done for today. Streak safe!", done: 1,
		},
		{
			name: "only rest days", slot: 0,
			habits:  []store.Habit{habit("hike", weekends), archived},
			subject: "😌 Rest day. Your streaks are safe", rest: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, ok := Build(tt.habits, today, Slots[tt.slot], "http://app")
			if !ok {
				t.Fatal("nothing built")
			}
			if d.Subject != tt.subject {
				t.Errorf("subject = %q, want %q", d.Subject, tt.subject)
			}
			var todo []string
			for _, l := range d.Todo {
				todo = append(todo, fmt.Sprintf("%s:%d:%t", l.Name, l.Streak, l.AtRisk))
			}
			if strings.Join(todo, ",") != strings.Join(tt.todo, ",") {
				t.Errorf("todo = %v, want %v", todo, tt.todo)
			}
			if len(d.Done) != tt.done || len(d.Rest) != tt.rest {
				t.Errorf("done=%d rest=%d, want %d %d", len(d.Done), len(d.Rest), tt.done, tt.rest)
			}
		})
	}
}

func TestBuildNothingToSay(t *testing.T) {
	archived := habit("old", nil)
	now := time.Now()
	archived.ArchivedAt = &now
	if _, ok := Build([]store.Habit{archived}, day("2026-11-04"), Slots[0], ""); ok {
		t.Error("archived habits only should send nothing")
	}
}

func TestMessageEscapesNames(t *testing.T) {
	d, _ := Build([]store.Habit{habit(`<script>alert(1)</script>`, nil)}, day("2026-11-04"), Slots[2], "http://app")
	m, err := d.Message("me@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.HTML, "<script>") || !strings.Contains(m.HTML, "&lt;script&gt;") {
		t.Error("habit name not escaped in HTML")
	}
	for _, want := range []string{"Good evening", "Wednesday, 4 November", "To do", "Start a streak today", "Daily streak: 0 days", "http://app"} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("text missing %q:\n%s", want, m.Text)
		}
	}
}

func TestDueSlot(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, streak.IST)
		if err != nil {
			panic(err)
		}
		return v.UTC() // The clock's zone mustn't matter.
	}
	tests := []struct {
		now  string
		slot string // "" means none due.
	}{
		{"2026-11-04 10:59", ""},
		{"2026-11-04 11:00", "morning"},
		{"2026-11-04 11:59", "morning"},
		{"2026-11-04 12:00", ""},
		{"2026-11-04 17:30", "afternoon"},
		{"2026-11-04 22:00", "night"},
		{"2026-11-04 22:59", "night"},
		{"2026-11-04 23:00", ""},
		{"2026-11-05 00:30", ""},
	}
	for _, tt := range tests {
		d, slot, ok := DueSlot(at(tt.now))
		got := ""
		if ok {
			got = slot.Name
			if d.String() != tt.now[:10] {
				t.Errorf("%s: day = %s", tt.now, d)
			}
		}
		if got != tt.slot {
			t.Errorf("%s: slot = %q, want %q", tt.now, got, tt.slot)
		}
	}
}

func TestLeadAndCta(t *testing.T) {
	today := day("2026-11-04")
	tests := []struct {
		name   string
		habits []store.Habit
		slot   int
		lead   string
		cta    string
	}{
		{"morning", []store.Habit{habit("gym", nil)}, 0, "On today: “gym”", "Check in now"},
		{"afternoon", []store.Habit{habit("a", nil, "2026-11-04"), habit("b", nil)}, 1, "1 down, 1 to go: “b”", "Check in now"},
		{"night, one streak at risk", []store.Habit{habit("a", nil, "2026-11-03")}, 2, "your 1-day streak ends at midnight", "Check in now"},
		{"night, two streaks at risk", []store.Habit{habit("a", nil, "2026-11-03"), habit("b", nil, "2026-11-03")}, 2, "those streaks end at midnight", "Check in now"},
		{"night, nothing at risk", []store.Habit{habit("a", nil)}, 2, "close out the day", "Check in now"},
		{"afternoon, nothing yet", []store.Habit{habit("a", nil), habit("b", nil)}, 1, "haven't checked in anything yet. Still to do: “a” and “b”.", "Check in now"},
		{"all done", []store.Habit{habit("a", nil, "2026-11-04")}, 0, "Every habit is checked off", "Open Streaks"},
		{"rest", []store.Habit{habit("a", []int{0, 6})}, 0, "Enjoy the break", "Open Streaks"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, ok := Build(tt.habits, today, Slots[tt.slot], "http://app")
			if !ok {
				t.Fatal("nothing built")
			}
			if !strings.Contains(d.Lead, tt.lead) {
				t.Errorf("lead = %q, want it to contain %q", d.Lead, tt.lead)
			}
			if d.Cta() != tt.cta {
				t.Errorf("cta = %q, want %q", d.Cta(), tt.cta)
			}
			m, err := d.Message("me@example.com")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(m.HTML, tt.cta) || !strings.Contains(m.Text, d.Lead) {
				t.Errorf("email missing cta or lead:\n%s", m.Text)
			}
		})
	}
}

func TestMessageWithoutAppURL(t *testing.T) {
	d, _ := Build([]store.Habit{habit("gym", nil)}, day("2026-11-04"), Slots[0], "")
	m, err := d.Message("me@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.HTML, "<a href") || strings.Contains(m.Text, "Open Streaks") {
		t.Error("no app URL should mean no button or link")
	}
}

func TestSubjectAfternoonNothingYet(t *testing.T) {
	d, _ := Build([]store.Habit{habit("a", nil)}, day("2026-11-04"), Slots[1], "")
	if d.Subject != "Nothing checked in yet. Today still counts" {
		t.Errorf("subject = %q", d.Subject)
	}
}

func TestSubjectSingleStreakAtRisk(t *testing.T) {
	d, _ := Build([]store.Habit{habit("a", nil, "2026-11-02", "2026-11-03")}, day("2026-11-04"), Slots[2], "")
	if d.Subject != "🔥 Your 2-day streak ends at midnight" {
		t.Errorf("subject = %q", d.Subject)
	}
}

func TestNames(t *testing.T) {
	l := func(n ...string) []Line {
		var out []Line
		for _, s := range n {
			out = append(out, Line{Name: s})
		}
		return out
	}
	tests := []struct {
		in   []Line
		want string
	}{
		{nil, ""},
		{l("a"), "“a”"},
		{l("a", "b"), "“a” and “b”"},
		{l("a", "b", "c"), "“a”, “b” and “c”"},
		{l("a", "b", "c", "d", "e"), "“a”, “b” and 3 more"},
	}
	for _, tt := range tests {
		if got := names(tt.in); got != tt.want {
			t.Errorf("names = %q, want %q", got, tt.want)
		}
	}
}

package streak

import (
	"testing"
	"time"
)

func day(t *testing.T, s string) Day {
	t.Helper()
	d, err := ParseDay(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func days(t *testing.T, ss ...string) []Day {
	t.Helper()
	out := make([]Day, 0, len(ss))
	for _, s := range ss {
		out = append(out, day(t, s))
	}
	return out
}

// 2026-11-02 is a Monday. Weekdays used below:
// Mon 02, Tue 03, Wed 04, Thu 05, Fri 06, Sat 07, Sun 08, Mon 09, Tue 10.
var (
	monToFri = MaskFromDays([]int{1, 2, 3, 4, 5})
	monWedFr = MaskFromDays([]int{1, 3, 5})
)

func TestCompute(t *testing.T) {
	type sched struct {
		from string
		mask uint8
	}
	daily := []sched{{"2026-10-01", EveryDay}}

	tests := []struct {
		name      string
		schedules []sched
		checkIns  []string
		today     string
		current   int
		longest   int
		last      string
	}{
		{
			name: "no check-ins", schedules: daily, today: "2026-11-04",
		},
		{
			name: "single check-in today", schedules: daily,
			checkIns: []string{"2026-11-04"}, today: "2026-11-04",
			current: 1, longest: 1, last: "2026-11-04",
		},
		{
			name: "consecutive days ending today", schedules: daily,
			checkIns: []string{"2026-11-02", "2026-11-03", "2026-11-04"}, today: "2026-11-04",
			current: 3, longest: 3, last: "2026-11-04",
		},
		{
			name: "unsorted check-ins give the same answer", schedules: daily,
			checkIns: []string{"2026-11-04", "2026-11-02", "2026-11-03"}, today: "2026-11-04",
			current: 3, longest: 3, last: "2026-11-04",
		},
		{
			name: "today pending does not break the streak", schedules: daily,
			checkIns: []string{"2026-11-02", "2026-11-03"}, today: "2026-11-04",
			current: 2, longest: 2, last: "2026-11-03",
		},
		{
			name: "missing yesterday breaks the streak", schedules: daily,
			checkIns: []string{"2026-11-01", "2026-11-02"}, today: "2026-11-04",
			current: 0, longest: 2, last: "2026-11-02",
		},
		{
			name: "gap resets current and longest is kept", schedules: daily,
			checkIns: []string{
				"2026-10-20", "2026-10-21", "2026-10-22", "2026-10-23",
				"2026-11-03", "2026-11-04",
			},
			today: "2026-11-04", current: 2, longest: 4, last: "2026-11-04",
		},
		{
			name: "current can equal a later longest", schedules: daily,
			checkIns: []string{
				"2026-10-20",
				"2026-11-01", "2026-11-02", "2026-11-03", "2026-11-04",
			},
			today: "2026-11-04", current: 4, longest: 4, last: "2026-11-04",
		},
		{
			name:      "weekend off does not break a weekday streak",
			schedules: []sched{{"2026-11-02", monToFri}},
			checkIns:  []string{"2026-11-05", "2026-11-06", "2026-11-09", "2026-11-10"},
			today:     "2026-11-10", current: 4, longest: 4, last: "2026-11-10",
		},
		{
			name:      "viewing on a non-target day keeps the streak",
			schedules: []sched{{"2026-11-02", monToFri}},
			checkIns:  []string{"2026-11-05", "2026-11-06"},
			today:     "2026-11-08", current: 2, longest: 2, last: "2026-11-06",
		},
		{
			name:      "non-target check-in holds the streak without adding",
			schedules: []sched{{"2026-11-05", monToFri}},
			checkIns:  []string{"2026-11-05", "2026-11-06", "2026-11-07", "2026-11-09"},
			today:     "2026-11-09", current: 3, longest: 3, last: "2026-11-09",
		},
		{
			name:      "non-target check-in alone updates only last completed",
			schedules: []sched{{"2026-11-07", monToFri}},
			checkIns:  []string{"2026-11-07"},
			today:     "2026-11-07", current: 0, longest: 0, last: "2026-11-07",
		},
		{
			name:      "non-target check-in cannot rescue a missed target day",
			schedules: []sched{{"2026-11-02", monWedFr}},
			checkIns:  []string{"2026-11-02", "2026-11-03", "2026-11-06"},
			today:     "2026-11-06", current: 1, longest: 1, last: "2026-11-06",
		},
		{
			name:      "backdated check-in fills a gap",
			schedules: daily,
			checkIns:  []string{"2026-11-02", "2026-11-04", "2026-11-03"},
			today:     "2026-11-04", current: 3, longest: 3, last: "2026-11-04",
		},
		{
			name:      "backdated check-in on a new habit extends its start",
			schedules: []sched{{"2026-11-04", EveryDay}},
			checkIns:  []string{"2026-11-02", "2026-11-03", "2026-11-04"},
			today:     "2026-11-04", current: 3, longest: 3, last: "2026-11-04",
		},
		{
			name:      "days before the habit started are not missed",
			schedules: []sched{{"2026-11-03", EveryDay}},
			checkIns:  []string{"2026-11-03", "2026-11-04"},
			today:     "2026-11-04", current: 2, longest: 2, last: "2026-11-04",
		},
		{
			name: "future check-ins are ignored", schedules: daily,
			checkIns: []string{"2026-11-04", "2026-11-05"}, today: "2026-11-04",
			current: 1, longest: 1, last: "2026-11-04",
		},
		{
			name: "changing target days does not rewrite history",
			schedules: []sched{
				{"2026-11-02", MaskFromDays([]int{1})}, // Mondays only.
				{"2026-11-09", EveryDay},
			},
			checkIns: []string{"2026-11-02", "2026-11-09", "2026-11-10"},
			today:    "2026-11-10", current: 3, longest: 3, last: "2026-11-10",
		},
		{
			name: "archiving holds the streak",
			schedules: []sched{
				{"2026-11-02", EveryDay},
				{"2026-11-04", 0},
			},
			checkIns: []string{"2026-11-02", "2026-11-03"},
			today:    "2026-11-10", current: 2, longest: 2, last: "2026-11-03",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := Habit{CheckIns: days(t, tc.checkIns...)}
			for _, s := range tc.schedules {
				h.Schedules = append(h.Schedules, Schedule{From: day(t, s.from), Mask: s.mask})
			}
			got := Compute(h, day(t, tc.today))
			if got.Current != tc.current || got.Longest != tc.longest {
				t.Errorf("current=%d longest=%d, want current=%d longest=%d",
					got.Current, got.Longest, tc.current, tc.longest)
			}
			switch {
			case tc.last == "" && got.LastCompleted != nil:
				t.Errorf("last=%s, want none", got.LastCompleted)
			case tc.last != "" && (got.LastCompleted == nil || got.LastCompleted.String() != tc.last):
				t.Errorf("last=%v, want %s", got.LastCompleted, tc.last)
			}
		})
	}
}

func TestMidnightISTBoundary(t *testing.T) {
	tests := []struct {
		utc  string
		want string
	}{
		{"2026-11-03T18:29:59Z", "2026-11-03"}, // 23:59:59 IST
		{"2026-11-03T18:30:00Z", "2026-11-04"}, // 00:00:00 IST
		{"2026-11-04T18:29:59Z", "2026-11-04"},
		{"2026-12-31T18:30:00Z", "2027-01-01"}, // Year rollover.
	}
	for _, tc := range tests {
		ts, err := time.Parse(time.RFC3339, tc.utc)
		if err != nil {
			t.Fatal(err)
		}
		if got := DayOf(ts).String(); got != tc.want {
			t.Errorf("DayOf(%s) = %s, want %s", tc.utc, got, tc.want)
		}
	}

	// One second either side of midnight IST decides whether a streak is alive.
	h := Habit{
		Schedules: []Schedule{{From: day(t, "2026-11-01"), Mask: EveryDay}},
		CheckIns:  days(t, "2026-11-02"),
	}
	before, _ := time.Parse(time.RFC3339, "2026-11-03T18:29:59Z") // Still the 3rd: 3rd is pending.
	after, _ := time.Parse(time.RFC3339, "2026-11-03T18:30:00Z")  // Now the 4th: 3rd was missed.
	if got := Compute(h, DayOf(before)).Current; got != 1 {
		t.Errorf("before midnight IST: current=%d, want 1", got)
	}
	if got := Compute(h, DayOf(after)).Current; got != 0 {
		t.Errorf("after midnight IST: current=%d, want 0", got)
	}
}

func TestComputeDaily(t *testing.T) {
	everyday := []Schedule{{From: day(t, "2026-11-02"), Mask: EveryDay}}
	weekdays := []Schedule{{From: day(t, "2026-11-02"), Mask: monToFri}}

	tests := []struct {
		name    string
		habits  []Habit
		today   string
		current int
		longest int
		last    string
	}{
		{name: "no habits", today: "2026-11-04"},
		{
			name: "all habits done on every day",
			habits: []Habit{
				{Schedules: everyday, CheckIns: days(t, "2026-11-02", "2026-11-03")},
				{Schedules: everyday, CheckIns: days(t, "2026-11-02", "2026-11-03")},
			},
			today: "2026-11-03", current: 2, longest: 2, last: "2026-11-03",
		},
		{
			name: "one habit missed breaks the day",
			habits: []Habit{
				{Schedules: everyday, CheckIns: days(t, "2026-11-02", "2026-11-03", "2026-11-04")},
				{Schedules: everyday, CheckIns: days(t, "2026-11-02", "2026-11-04")},
			},
			today: "2026-11-04", current: 1, longest: 1, last: "2026-11-04",
		},
		{
			name: "today partly done is pending",
			habits: []Habit{
				{Schedules: everyday, CheckIns: days(t, "2026-11-02", "2026-11-03")},
				{Schedules: everyday, CheckIns: days(t, "2026-11-02")},
			},
			today: "2026-11-03", current: 1, longest: 1, last: "2026-11-02",
		},
		{
			name: "habit not scheduled that day is not required",
			habits: []Habit{
				{Schedules: everyday, CheckIns: days(t, "2026-11-06", "2026-11-07", "2026-11-08")},
				{Schedules: weekdays, CheckIns: days(t, "2026-11-06")},
			},
			today: "2026-11-08", current: 3, longest: 3, last: "2026-11-08",
		},
		{
			name:   "days with nothing scheduled are skipped",
			habits: []Habit{{Schedules: weekdays, CheckIns: days(t, "2026-11-06", "2026-11-09")}},
			today:  "2026-11-09", current: 2, longest: 2, last: "2026-11-09",
		},
		{
			name: "archived habit stops being required",
			habits: []Habit{
				{Schedules: everyday, CheckIns: days(t, "2026-11-02", "2026-11-03", "2026-11-04")},
				{
					Schedules: []Schedule{
						{From: day(t, "2026-11-02"), Mask: EveryDay},
						{From: day(t, "2026-11-03"), Mask: 0},
					},
					CheckIns: days(t, "2026-11-02"),
				},
			},
			today: "2026-11-04", current: 3, longest: 3, last: "2026-11-04",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeDaily(tc.habits, day(t, tc.today))
			if got.Current != tc.current || got.Longest != tc.longest {
				t.Errorf("current=%d longest=%d, want current=%d longest=%d",
					got.Current, got.Longest, tc.current, tc.longest)
			}
			if tc.last != "" && (got.LastCompleted == nil || got.LastCompleted.String() != tc.last) {
				t.Errorf("last=%v, want %s", got.LastCompleted, tc.last)
			}
		})
	}
}

func TestWeekday(t *testing.T) {
	tests := map[string]int{"2026-11-08": 0, "2026-11-02": 1, "2026-11-07": 6, "1970-01-01": 4, "1969-12-28": 0}
	for s, want := range tests {
		if got := day(t, s).Weekday(); got != want {
			t.Errorf("%s weekday = %d, want %d", s, got, want)
		}
	}
}

func TestMaskRoundTrip(t *testing.T) {
	if MaskFromDays(nil) != EveryDay {
		t.Error("no target days should mean every day")
	}
	got := DaysFromMask(MaskFromDays([]int{1, 3, 5}))
	if len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 5 {
		t.Errorf("round trip = %v", got)
	}
}

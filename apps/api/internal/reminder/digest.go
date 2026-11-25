package reminder

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"

	"streaks/internal/mailer"
	"streaks/internal/store"
	"streaks/internal/streak"
)

// Slot is one of the day's reminder times, in IST.
type Slot struct {
	Name     string // Stored, so it must not change.
	Hour     int
	Greeting string
}

// Slots are the reminder times: late morning, late afternoon, and two hours
// before midnight, when a streak can still be saved.
var Slots = []Slot{
	{Name: "morning", Hour: 11, Greeting: "Good morning"},
	{Name: "afternoon", Hour: 17, Greeting: "Good afternoon"},
	{Name: "night", Hour: 22, Greeting: "Good evening"},
}

// Line is one habit in the email.
type Line struct {
	Name   string
	Status string // "todo", "done" or "rest".
	Streak int
	AtRisk bool // Not done yet, with a streak that ends if today is missed.
}

// Digest is what one email says.
type Digest struct {
	Slot    Slot
	Date    string
	Todo    []Line
	Done    []Line
	Rest    []Line
	Daily   streak.Result
	AppURL  string
	Subject string
	Lead    string // The one-line message under the greeting.
}

// Build summarizes today's habits. ok is false when there are no active habits,
// because then there's nothing to say.
func Build(habits []store.Habit, today streak.Day, slot Slot, appURL string) (d Digest, ok bool) {
	d = Digest{Slot: slot, Date: formatDay(today), AppURL: appURL}
	var inputs []streak.Habit
	for _, h := range habits {
		if h.ArchivedAt != nil {
			continue
		}
		input := h.StreakInput()
		inputs = append(inputs, input)
		t := streak.NewTracker(input, today)
		line := Line{Name: h.Name, Streak: streak.Compute(input, today).Current}
		switch {
		case t.Done(today):
			line.Status = "done"
			d.Done = append(d.Done, line)
		case t.Scheduled(today) && !t.CoolOff(today):
			line.Status = "todo"
			line.AtRisk = line.Streak > 0
			d.Todo = append(d.Todo, line)
		default:
			line.Status = "rest"
			d.Rest = append(d.Rest, line)
		}
	}
	if len(inputs) == 0 {
		return d, false
	}
	d.Daily = streak.ComputeDaily(inputs, today)
	d.Subject = d.headline()
	d.Lead = d.lead()
	return d, true
}

func (d Digest) atRisk() int {
	n := 0
	for _, l := range d.Todo {
		if l.AtRisk {
			n++
		}
	}
	return n
}

// riskStreak is the longest streak that ends tonight unless it's checked in.
func (d Digest) riskStreak() int {
	n := 0
	for _, l := range d.Todo {
		if l.AtRisk && l.Streak > n {
			n = l.Streak
		}
	}
	return n
}

// headline is the subject line.
func (d Digest) headline() string {
	todo, done := len(d.Todo), len(d.Done)
	switch {
	case todo == 0 && done == 0:
		return "😌 Rest day. Your streaks are safe"
	case todo == 0:
		return "🎉 All done for today. Streak safe!"
	case d.Slot.Name == "night" && d.atRisk() == 1:
		return fmt.Sprintf("🔥 Your %s ends at midnight", dayStreak(d.riskStreak()))
	case d.Slot.Name == "night" && d.atRisk() > 1:
		return fmt.Sprintf("🔥 %d streaks end at midnight, up to %s", d.atRisk(), days(d.riskStreak()))
	case d.Slot.Name == "night":
		return fmt.Sprintf("%s left. Finish strong", plural(todo, "habit"))
	case d.Slot.Name == "afternoon" && done == 0:
		return "Nothing checked in yet. Today still counts"
	case d.Slot.Name == "afternoon":
		return fmt.Sprintf("%d of %d done. Keep going!", done, todo+done)
	}
	return fmt.Sprintf("%s today. Start with the easy one", plural(todo, "habit"))
}

// lead is the sentence at the top of the email body.
func (d Digest) lead() string {
	todo, done := len(d.Todo), len(d.Done)
	open := names(d.Todo)
	switch {
	case todo == 0 && done == 0:
		return "Nothing is scheduled today, so your streaks are safe. Enjoy the break."
	case todo == 0:
		return "Every habit is checked off and your streak is safe. See you tomorrow."
	case d.Slot.Name == "night" && d.atRisk() == 1:
		return fmt.Sprintf("%s is still open, and your %s ends at midnight. A couple of minutes is enough.", open, dayStreak(d.riskStreak()))
	case d.Slot.Name == "night" && d.atRisk() > 1:
		return fmt.Sprintf("%s are still open, and those streaks end at midnight. Don't let them go now.", open)
	case d.Slot.Name == "night":
		return fmt.Sprintf("Still open: %s. Check in before midnight and close out the day.", open)
	case d.Slot.Name == "afternoon" && done == 0:
		return fmt.Sprintf("You haven't checked in anything yet. Still to do: %s.", open)
	case d.Slot.Name == "afternoon":
		return fmt.Sprintf("%d down, %d to go: %s.", done, todo, open)
	}
	return fmt.Sprintf("On today: %s. Get it done early and the day is yours.", open)
}

// names lists habit names for a sentence: "a", "a and b", "a, b and c",
// "a, b and 2 more".
func names(lines []Line) string {
	q := func(l Line) string { return "\u201c" + l.Name + "\u201d" }
	switch len(lines) {
	case 0:
		return ""
	case 1:
		return q(lines[0])
	case 2:
		return q(lines[0]) + " and " + q(lines[1])
	case 3:
		return q(lines[0]) + ", " + q(lines[1]) + " and " + q(lines[2])
	}
	return q(lines[0]) + ", " + q(lines[1]) + " and " + fmt.Sprintf("%d more", len(lines)-2)
}

func dayStreak(n int) string { return fmt.Sprintf("%d-day streak", n) }

func (d Digest) Cta() string {
	if len(d.Todo) == 0 {
		return "Open Streaks"
	}
	return "Check in now"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func days(n int) string { return plural(n, "day") }

func formatDay(d streak.Day) string {
	t, _ := time.Parse("2006-01-02", d.String())
	return t.Format("Monday, 2 January")
}

// Message renders the digest for one recipient.
func (d Digest) Message(to string) (mailer.Message, error) {
	var html bytes.Buffer
	if err := htmlTemplate.Execute(&html, d); err != nil {
		return mailer.Message{}, err
	}
	return mailer.Message{To: to, Subject: d.Subject, Text: d.text(), HTML: html.String()}, nil
}

func (l Line) describe() string {
	switch {
	case l.AtRisk:
		return fmt.Sprintf("%d-day streak. Do it today to keep it.", l.Streak)
	case l.Streak > 0:
		return fmt.Sprintf("%d-day streak", l.Streak)
	case l.Status == "todo":
		return "Start a streak today"
	}
	return "No streak yet"
}

func (d Digest) text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s. It's %s.\n\n%s\n", d.Slot.Greeting, d.Date, d.Lead)
	section := func(title string, lines []Line) {
		if len(lines) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s\n", title)
		for _, l := range lines {
			fmt.Fprintf(&b, "- %s: %s\n", l.Name, l.describe())
		}
	}
	section("To do", d.Todo)
	section("Done", d.Done)
	section("Rest day (streak held)", d.Rest)
	fmt.Fprintf(&b, "\nDaily streak: %s (longest %s)\n", days(d.Daily.Current), days(d.Daily.Longest))
	if d.AppURL != "" {
		fmt.Fprintf(&b, "\nOpen Streaks: %s\n", d.AppURL)
	}
	return b.String()
}

var htmlTemplate = template.Must(template.New("digest").Funcs(template.FuncMap{
	"describe": Line.describe,
	"days":     days,
	"list":     func(v ...any) []any { return v },
}).Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light"></head>
<body style="margin:0;padding:24px 16px;background:#ffffff;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#1c1917;font-size:16px;line-height:24px">
<div style="display:none;max-height:0;overflow:hidden;opacity:0">{{.Lead}}</div>
<div style="max-width:480px;margin:0 auto">
<p style="margin:0 0 24px;font-size:14px;font-weight:600;color:#059669">Streaks</p>
<h1 style="margin:0 0 8px;font-size:22px;line-height:28px">{{.Slot.Greeting}}</h1>
<p style="margin:0 0 24px;color:#57534e">{{.Lead}}</p>
{{template "section" (list "To do" .Todo)}}
{{template "section" (list "Done" .Done)}}
{{template "section" (list "Rest day" .Rest)}}
{{if .AppURL}}<p style="margin:24px 0"><a href="{{.AppURL}}" style="display:inline-block;background:#059669;color:#ffffff;text-decoration:none;padding:12px 20px;border-radius:8px;font-weight:600">{{.Cta}}</a></p>{{end}}
<p style="margin:24px 0 0;padding-top:16px;border-top:1px solid #e7e5e4;font-size:13px;color:#78716c">{{.Date}}. Daily streak: {{days .Daily.Current}} (longest {{days .Daily.Longest}}). You can turn these emails off in Settings.</p>
</div></body></html>
{{define "section"}}{{$lines := index . 1}}{{if $lines}}<p style="margin:16px 0 4px;font-size:13px;font-weight:600;color:#78716c">{{index . 0}}</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0">{{range $lines}}
<tr><td style="padding:8px 0;border-bottom:1px solid #f5f5f4"><strong>{{.Name}}</strong><br><span style="font-size:14px;color:{{if .AtRisk}}#dc2626{{else}}#78716c{{end}}">{{describe .}}</span></td></tr>{{end}}
</table>{{end}}{{end}}`))

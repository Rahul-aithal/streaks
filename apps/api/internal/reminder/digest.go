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
		input := streak.Habit{Schedules: h.Schedules, CheckIns: h.CheckIns}
		inputs = append(inputs, input)
		t := streak.NewTracker(input, today)
		line := Line{Name: h.Name, Streak: streak.Compute(input, today).Current}
		switch {
		case t.Done(today):
			line.Status = "done"
			d.Done = append(d.Done, line)
		case t.Scheduled(today):
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
	d.Subject = "Streaks: " + d.headline()
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

func (d Digest) headline() string {
	todo, done := len(d.Todo), len(d.Done)
	switch {
	case todo == 0 && done == 0:
		return "rest day, your streaks are safe"
	case todo == 0:
		return fmt.Sprintf("all %s done today", plural(done, "habit"))
	case d.Slot.Name == "night" && d.atRisk() > 0:
		return fmt.Sprintf("%s ending at midnight", plural(d.atRisk(), "streak"))
	case d.Slot.Name == "night":
		return fmt.Sprintf("%s left today", plural(todo, "habit"))
	case d.Slot.Name == "afternoon":
		return fmt.Sprintf("%d of %s done today", done, plural(todo+done, "habit"))
	}
	return fmt.Sprintf("%s to do today", plural(todo, "habit"))
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
	fmt.Fprintf(&b, "%s. Here's %s.\n", d.Slot.Greeting, d.Date)
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
<html><body style="margin:0;padding:24px;background:#f5f5f4;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#1c1917">
<div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e7e5e4;border-radius:12px;padding:24px">
<p style="margin:0 0 4px;color:#059669;font-weight:600">Streaks</p>
<h1 style="margin:0 0 4px;font-size:20px">{{.Slot.Greeting}}</h1>
<p style="margin:0 0 20px;color:#78716c">{{.Date}}</p>
{{template "section" (list "To do" .Todo)}}
{{template "section" (list "Done" .Done)}}
{{template "section" (list "Rest day, streak held" .Rest)}}
<p style="margin:20px 0 0;padding-top:16px;border-top:1px solid #e7e5e4">Daily streak: <strong>{{days .Daily.Current}}</strong> <span style="color:#78716c">(longest {{days .Daily.Longest}})</span></p>
{{if .AppURL}}<p style="margin:20px 0 0"><a href="{{.AppURL}}" style="display:inline-block;background:#059669;color:#ffffff;text-decoration:none;padding:10px 16px;border-radius:8px;font-weight:600">Open Streaks</a></p>{{end}}
</div></body></html>
{{define "section"}}{{$lines := index . 1}}{{if $lines}}<h2 style="margin:16px 0 8px;font-size:14px;text-transform:uppercase;letter-spacing:.04em;color:#78716c">{{index . 0}}</h2>
<ul style="margin:0;padding:0;list-style:none">{{range $lines}}
<li style="padding:8px 0;border-bottom:1px solid #f5f5f4"><strong>{{.Name}}</strong><br><span style="color:{{if .AtRisk}}#dc2626{{else}}#78716c{{end}};font-size:14px">{{describe .}}</span></li>{{end}}
</ul>{{end}}{{end}}`))

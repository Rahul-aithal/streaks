// Package httpapi exposes the JSON API.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"streaks/internal/gtasks"
	"streaks/internal/store"
	"streaks/internal/streak"
	"streaks/internal/syncer"
)

const (
	backdateWindow = 2 // Days a manual check-in can reach back.
	maxHistoryDays = 800
)

// Server holds the API's dependencies.
type Server struct {
	Store *store.Store
	Sync  *syncer.Syncer
	Now   func() time.Time

	AllowedEmails []string // Lowercase. Only these Google accounts may sign in.
	WebOrigin     string   // Browser origin allowed to make state-changing requests.
	SecureCookies bool

	oauthStates *stateStore
}

// Handler returns the public login routes and the session-protected API.
func (s *Server) Handler() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /habits", s.listHabits)
	api.HandleFunc("POST /habits", s.createHabit)
	api.HandleFunc("GET /habits/{id}", s.getHabit)
	api.HandleFunc("PATCH /habits/{id}", s.updateHabit)
	api.HandleFunc("DELETE /habits/{id}", s.deleteHabit)
	api.HandleFunc("POST /habits/{id}/archive", s.archiveHabit)
	api.HandleFunc("POST /habits/{id}/unarchive", s.unarchiveHabit)
	api.HandleFunc("PUT /habits/{id}/check-ins/{day}", s.putCheckIn)
	api.HandleFunc("DELETE /habits/{id}/check-ins/{day}", s.deleteCheckIn)
	api.HandleFunc("GET /habits/{id}/history", s.habitHistory)
	api.HandleFunc("GET /streaks/daily", s.dailyStreak)
	api.HandleFunc("POST /sync/github", s.syncNow(syncer.KindGitHub))
	api.HandleFunc("POST /sync/google-tasks", s.syncNow(syncer.KindGoogle))
	api.HandleFunc("GET /summary/today", s.summaryToday)
	api.HandleFunc("GET /auth/me", s.me)
	api.HandleFunc("POST /auth/switch", s.switchAccount)
	api.HandleFunc("POST /auth/sign-out", s.signOut)
	api.HandleFunc("POST /auth/sign-out-all", s.signOutAll)
	api.HandleFunc("DELETE /account", s.deleteAccount)
	api.HandleFunc("PUT /settings/github", s.linkGitHub)
	api.HandleFunc("DELETE /settings/github", s.unlinkGitHub)

	s.oauthStates = newStateStore(s.Now)

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"}) // No DB call: must not wake Neon.
	})
	// Public: these create the session. The state value and cookie protect the callback.
	root.HandleFunc("GET /auth/google/start", s.startGoogleLogin)
	root.HandleFunc("GET /oauth/google/callback", s.googleCallback)
	root.Handle("/", s.requireSession(api))
	return s.checkOrigin(root)
}

func (s *Server) today() streak.Day { return streak.DayOf(s.Now()) }

// --- Habits ---

type habitJSON struct {
	ID         int64         `json:"id"`
	Name       string        `json:"name"`
	Source     string        `json:"source"`
	TargetDays []int         `json:"target_days"`
	Archived   bool          `json:"archived"`
	ArchivedAt *time.Time    `json:"archived_at"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
	Streak     streak.Result `json:"streak"`
}

func toHabitJSON(h store.Habit, today streak.Day) habitJSON {
	return habitJSON{
		ID:         h.ID,
		Name:       h.Name,
		Source:     h.Source,
		TargetDays: streak.DaysFromMask(targetMask(h.Schedules)),
		Archived:   h.ArchivedAt != nil,
		ArchivedAt: h.ArchivedAt,
		CreatedAt:  h.CreatedAt,
		UpdatedAt:  h.UpdatedAt,
		Streak:     streak.Compute(streakInput(h), today),
	}
}

func streakInput(h store.Habit) streak.Habit {
	return streak.Habit{Schedules: h.Schedules, CheckIns: h.CheckIns}
}

// targetMask is the newest non-archived mask, so an archived habit still shows
// the days it targeted.
func targetMask(schedules []streak.Schedule) uint8 {
	var latest streak.Day
	mask := streak.EveryDay
	found := false
	for _, sc := range schedules {
		if sc.Mask != 0 && (!found || sc.From > latest) {
			latest, mask, found = sc.From, sc.Mask, true
		}
	}
	return mask
}

func (s *Server) listHabits(w http.ResponseWriter, r *http.Request) {
	habits, err := s.Store.ListHabits(r.Context(), userID(r))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	today := s.today()
	out := make([]habitJSON, 0, len(habits))
	for _, h := range habits {
		out = append(out, toHabitJSON(h, today))
	}
	writeJSON(w, http.StatusOK, out)
}

type createHabitRequest struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	TargetDays []int  `json:"target_days"`
}

func (s *Server) createHabit(w http.ResponseWriter, r *http.Request) {
	var req createHabitRequest
	if !readJSON(w, r, &req) {
		return
	}
	name, err := cleanName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Source == "" {
		req.Source = "manual"
	}
	if req.Source != "manual" && req.Source != "github" {
		writeError(w, http.StatusBadRequest, `source must be "manual" or "github"`)
		return
	}
	if err := validateDays(req.TargetDays); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	today := s.today()
	id, err := s.Store.CreateHabit(r.Context(), userID(r), name, req.Source, streak.MaskFromDays(req.TargetDays), today)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h, err := s.Store.GetHabit(r.Context(), userID(r), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toHabitJSON(h, today))
}

func (s *Server) getHabit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.respondHabit(w, r, id)
}

func (s *Server) respondHabit(w http.ResponseWriter, r *http.Request, id int64) {
	h, err := s.Store.GetHabit(r.Context(), userID(r), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toHabitJSON(h, s.today()))
}

type updateHabitRequest struct {
	Name       *string `json:"name"`
	TargetDays *[]int  `json:"target_days"`
}

func (s *Server) updateHabit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req updateHabitRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Name != nil {
		name, err := cleanName(*req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		req.Name = &name
	}
	var mask *uint8
	if req.TargetDays != nil {
		if err := validateDays(*req.TargetDays); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		m := streak.MaskFromDays(*req.TargetDays)
		mask = &m
	}
	existing, err := s.Store.GetHabit(r.Context(), userID(r), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if existing.ArchivedAt != nil && mask != nil {
		writeError(w, http.StatusConflict, "unarchive the habit before changing its target days")
		return
	}
	if err := s.Store.UpdateHabit(r.Context(), userID(r), id, req.Name, mask, s.today()); err != nil {
		writeStoreError(w, err)
		return
	}
	s.respondHabit(w, r, id)
}

func (s *Server) deleteHabit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteHabit(r.Context(), userID(r), id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) archiveHabit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	h, err := s.Store.GetHabit(r.Context(), userID(r), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if h.ArchivedAt == nil { // Archiving twice must not add another schedule row.
		if err := s.Store.ArchiveHabit(r.Context(), userID(r), id, s.today()); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	s.respondHabit(w, r, id)
}

func (s *Server) unarchiveHabit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	h, err := s.Store.GetHabit(r.Context(), userID(r), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if h.ArchivedAt != nil {
		if err := s.Store.UnarchiveHabit(r.Context(), userID(r), id, s.today()); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	s.respondHabit(w, r, id)
}

// --- Check-ins ---

// validateCheckInDay enforces the manual window: today back to today-2.
func validateCheckInDay(day, today streak.Day) error {
	if day > today {
		return errors.New("cannot check in for a future date")
	}
	if day < today-backdateWindow {
		return errors.New("can only check in up to 2 days back")
	}
	return nil
}

func (s *Server) checkInTarget(w http.ResponseWriter, r *http.Request) (int64, streak.Day, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return 0, 0, false
	}
	day, err := streak.ParseDay(r.PathValue("day"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return 0, 0, false
	}
	if err := validateCheckInDay(day, s.today()); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return 0, 0, false
	}
	h, err := s.Store.GetHabit(r.Context(), userID(r), id)
	if err != nil {
		writeStoreError(w, err)
		return 0, 0, false
	}
	if h.Source == "github" {
		writeError(w, http.StatusConflict, "GitHub habits are checked in by the sync")
		return 0, 0, false
	}
	if h.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "habit is archived")
		return 0, 0, false
	}
	return id, day, true
}

func (s *Server) putCheckIn(w http.ResponseWriter, r *http.Request) {
	id, day, ok := s.checkInTarget(w, r)
	if !ok {
		return
	}
	if err := s.Store.PutCheckIn(r.Context(), userID(r), id, day); err != nil {
		writeStoreError(w, err)
		return
	}
	s.respondHabit(w, r, id)
}

func (s *Server) deleteCheckIn(w http.ResponseWriter, r *http.Request) {
	id, day, ok := s.checkInTarget(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteCheckIn(r.Context(), userID(r), id, day); err != nil {
		writeStoreError(w, err)
		return
	}
	s.respondHabit(w, r, id)
}

// --- History and streaks ---

type historyDay struct {
	Day           streak.Day `json:"day"`
	Scheduled     bool       `json:"scheduled"`
	Done          bool       `json:"done"`
	Contributions *int       `json:"contributions,omitempty"` // GitHub habits only.
}

func (s *Server) habitHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	today := s.today()
	from, to, err := parseRange(r, today)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h, err := s.Store.GetHabit(r.Context(), userID(r), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var counts map[streak.Day]int
	if h.Source == "github" {
		if counts, err = s.Store.ContributionCounts(r.Context(), userID(r), from, to); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	tracker := streak.NewTracker(streakInput(h), today)
	days := make([]historyDay, 0, int(to-from)+1)
	for d := from; d <= to; d++ {
		item := historyDay{Day: d, Scheduled: tracker.Scheduled(d), Done: tracker.Done(d)}
		if h.Source == "github" {
			n := counts[d]
			item.Contributions = &n
		}
		days = append(days, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"habit": toHabitJSON(h, today), "days": days})
}

func parseRange(r *http.Request, today streak.Day) (from, to streak.Day, err error) {
	to, from = today, today-364
	if v := r.URL.Query().Get("to"); v != "" {
		if to, err = streak.ParseDay(v); err != nil {
			return 0, 0, err
		}
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if from, err = streak.ParseDay(v); err != nil {
			return 0, 0, err
		}
	}
	if from > to {
		return 0, 0, errors.New("from must not be after to")
	}
	if to-from > maxHistoryDays {
		return 0, 0, errors.New("date range is too long")
	}
	return from, to, nil
}

func dailyInputs(habits []store.Habit) []streak.Habit {
	out := make([]streak.Habit, 0, len(habits))
	for _, h := range habits {
		out = append(out, streakInput(h))
	}
	return out
}

func (s *Server) dailyStreak(w http.ResponseWriter, r *http.Request) {
	habits, err := s.Store.ListHabits(r.Context(), userID(r))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, streak.ComputeDaily(dailyInputs(habits), s.today()))
}

// --- Sync ---

func (s *Server) syncNow(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := s.Sync.Sync(r.Context(), userID(r), kind)
		switch {
		case errors.Is(err, syncer.ErrNotConfigured):
			writeError(w, http.StatusConflict, "Link your GitHub account in Settings first")
		case err != nil:
			slog.Error("manual sync failed", "kind", kind, "user", userID(r), "err", err)
			writeError(w, http.StatusBadGateway, err.Error())
		default:
			run, err := s.Store.GetSyncRun(r.Context(), userID(r), kind)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, run)
		}
	}
}

// --- Summary ---

type recentDay struct {
	Day   streak.Day `json:"day"`
	State string     `json:"state"` // "done", "bonus", "missed", "pending" or "rest".
}

const summaryRecentDays = 7

func dayState(t *streak.Tracker, d, today streak.Day) string {
	scheduled, done := t.Scheduled(d), t.Done(d)
	switch {
	case done && scheduled:
		return "done"
	case done:
		return "bonus"
	case !scheduled:
		return "rest"
	case d < today:
		return "missed"
	}
	return "pending"
}

type summaryHabit struct {
	ID            int64         `json:"id"`
	Name          string        `json:"name"`
	Source        string        `json:"source"`
	Status        string        `json:"status"` // "done", "pending", or "rest" on a non-target day.
	Contributions *int          `json:"contributions,omitempty"`
	Streak        streak.Result `json:"streak"`
	Recent        []recentDay   `json:"recent"`
}

type summaryTask struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Status      string      `json:"status"`
	Due         *streak.Day `json:"due"`
	Overdue     bool        `json:"overdue"`
	CompletedAt *time.Time  `json:"completed_at"`
}

func (s *Server) summaryToday(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := sessionFrom(r)
	id := sess.user.ID
	today := s.today()

	habits, err := s.Store.ListHabits(ctx, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	cached, err := s.Store.ListTasks(ctx, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	counts, err := s.Store.ContributionCounts(ctx, id, today, today)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	googleRun, err := s.Store.GetSyncRun(ctx, id, syncer.KindGoogle)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	githubRun, err := s.Store.GetSyncRun(ctx, id, syncer.KindGitHub)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	items := []summaryHabit{}
	for _, h := range habits {
		if h.ArchivedAt != nil {
			continue
		}
		tracker := streak.NewTracker(streakInput(h), today)
		item := summaryHabit{ID: h.ID, Name: h.Name, Source: h.Source, Streak: streak.Compute(streakInput(h), today)}
		for d := today - summaryRecentDays + 1; d <= today; d++ {
			item.Recent = append(item.Recent, recentDay{Day: d, State: dayState(tracker, d, today)})
		}
		switch {
		case tracker.Done(today):
			item.Status = "done"
		case tracker.Scheduled(today):
			item.Status = "pending"
		default:
			item.Status = "rest"
		}
		if h.Source == "github" {
			n := counts[today]
			item.Contributions = &n
		}
		items = append(items, item)
	}

	todayTasks, snap := gtasks.ForToday(cached, today)
	taskItems := make([]summaryTask, 0, len(todayTasks))
	for _, t := range todayTasks {
		taskItems = append(taskItems, summaryTask{
			ID: t.GoogleID, Title: t.Title, Status: t.Status, Due: t.Due, CompletedAt: t.CompletedAt,
			Overdue: t.Status != "completed" && t.Due != nil && *t.Due < today,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"date":   today,
		"habits": items,
		"tasks": map[string]any{
			"total":           snap.Total,
			"completed":       snap.Completed,
			"overdue":         snap.Overdue,
			"items":           taskItems,
			"sync":            googleRun,
			"needs_reconnect": sess.user.NeedsReconnect,
		},
		"streaks": map[string]any{
			"daily": streak.ComputeDaily(dailyInputs(habits), today),
		},
		"github": map[string]any{
			"linked":        sess.user.GitHubLogin != nil,
			"contributions": counts[today],
			"sync":          githubRun,
		},
	})
}

// --- Helpers ---

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is required")
	}
	if len(name) > 100 {
		return "", errors.New("name must be at most 100 characters")
	}
	return name, nil
}

func validateDays(days []int) error {
	for _, d := range days {
		if d < 0 || d > 6 {
			return errors.New("target_days must be weekday numbers from 0 (Sunday) to 6 (Saturday)")
		}
	}
	return nil
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid habit id")
		return 0, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "habit not found")
		return
	}
	if errors.Is(err, context.Canceled) {
		return // The client went away.
	}
	slog.Error("database error", "err", err)
	writeError(w, http.StatusServiceUnavailable, "database unavailable, try again")
}

package httpapi

import (
	"net/http"

	"streaks/internal/store"
	"streaks/internal/streak"
)

const (
	// Days back a reward can reach: further than a free check-in, so points can
	// repair a streak that broke earlier in the week.
	rewardBack         = 7
	coolOffAhead       = 14 // Days ahead a cool-off day can be booked.
	rewardHistoryLimit = 20
)

type coolOffJSON struct {
	Day streak.Day `json:"day"`
	// Only a day that hasn't started can be cancelled for a refund.
	Refundable bool `json:"refundable"`
}

func (s *Server) getRewards(w http.ResponseWriter, r *http.Request) {
	ctx, id, today := r.Context(), userID(r), s.today()
	wallet, err := s.Store.GetWallet(ctx, id, today)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	history, err := s.Store.RewardHistory(ctx, id, rewardHistoryLimit)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	days, err := s.Store.CoolOffDays(ctx, id, today-rewardBack)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	coolOffs := make([]coolOffJSON, 0, len(days))
	for _, d := range days {
		coolOffs = append(coolOffs, coolOffJSON{Day: d, Refundable: d > today})
	}
	if history == nil {
		history = []store.RewardEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"wallet":    wallet,
		"daily_cap": store.PointsPerDayCap,
		"costs":     map[string]int{"cool_off": store.CoolOffCost, "habit_save": store.SaveCost},
		"window":    map[string]int{"back": rewardBack, "ahead": coolOffAhead},
		"cool_offs": coolOffs,
		"history":   history,
	})
}

func (s *Server) rewardDay(w http.ResponseWriter, r *http.Request) (streak.Day, bool) {
	day, err := streak.ParseDay(r.PathValue("day"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return 0, false
	}
	return day, true
}

func (s *Server) buyCoolOff(w http.ResponseWriter, r *http.Request) {
	day, ok := s.rewardDay(w, r)
	if !ok {
		return
	}
	today := s.today()
	if day < today-rewardBack || day > today+coolOffAhead {
		writeError(w, http.StatusUnprocessableEntity, "a cool-off day must be within 7 days back or 14 days ahead")
		return
	}
	if err := s.Store.SpendCoolOff(r.Context(), userID(r), day); err != nil {
		writeStoreError(w, err)
		return
	}
	s.getRewards(w, r)
}

func (s *Server) refundCoolOff(w http.ResponseWriter, r *http.Request) {
	day, ok := s.rewardDay(w, r)
	if !ok {
		return
	}
	today := s.today()
	if day <= today {
		writeError(w, http.StatusConflict, "a cool-off day that has started can't be cancelled")
		return
	}
	if err := s.Store.RefundCoolOff(r.Context(), userID(r), day, today); err != nil {
		writeStoreError(w, err)
		return
	}
	s.getRewards(w, r)
}

// buySave marks a habit done on a day it was scheduled but missed, up to a week
// back. GitHub habits can be saved too.
func (s *Server) buySave(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	day, ok := s.rewardDay(w, r)
	if !ok {
		return
	}
	today := s.today()
	if day > today || day < today-rewardBack {
		writeError(w, http.StatusUnprocessableEntity, "you can save a day up to 7 days back")
		return
	}
	h, err := s.Store.GetHabit(r.Context(), userID(r), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if h.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "habit is archived")
		return
	}
	t := streak.NewTracker(h.StreakInput(), today)
	switch {
	case !t.Scheduled(day):
		writeError(w, http.StatusConflict, "that day isn't scheduled, so there's no streak to save")
		return
	case t.Done(day):
		writeError(w, http.StatusConflict, "that day is already done")
		return
	}
	if err := s.Store.SpendSave(r.Context(), userID(r), id, day); err != nil {
		writeStoreError(w, err)
		return
	}
	s.respondHabit(w, r, id)
}

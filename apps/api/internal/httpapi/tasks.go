package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"streaks/internal/gtasks"
	"streaks/internal/store"
	"streaks/internal/syncer"
)

func (s *Server) completeTask(w http.ResponseWriter, r *http.Request) { s.setTaskDone(w, r, true) }
func (s *Server) reopenTask(w http.ResponseWriter, r *http.Request)   { s.setTaskDone(w, r, false) }

// setTaskDone changes a task in Google Tasks. Repeating it changes nothing.
func (s *Server) setTaskDone(w http.ResponseWriter, r *http.Request, done bool) {
	id := r.PathValue("id")
	if id == "" || len(id) > 200 {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	t, err := s.Sync.SetTaskDone(r.Context(), userID(r), id, done)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "task not found: sync Google Tasks and try again")
	case errors.Is(err, gtasks.ErrTaskGone):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, gtasks.ErrNeedsWriteAccess):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, gtasks.ErrReconnect), errors.Is(err, syncer.ErrUnreadableToken):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		if r.Context().Err() != nil {
			return // The client went away.
		}
		slog.Error("update task", "user", userID(r), "err", err)
		writeError(w, http.StatusBadGateway, "couldn't update the task in Google Tasks, try again")
	default:
		today := s.today()
		writeJSON(w, http.StatusOK, summaryTask{
			ID: t.GoogleID, Title: t.Title, Status: t.Status, Due: t.Due, CompletedAt: t.CompletedAt,
			Overdue: t.Status != "completed" && t.Due != nil && *t.Due < today,
		})
	}
}

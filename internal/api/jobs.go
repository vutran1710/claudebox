package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/store"
)

type jobView struct {
	ID        string `json:"job"`
	Session   string `json:"session"`
	Status    string `json:"status"`
	Answer    string `json:"answer,omitempty"`
	Error     string `json:"error,omitempty"`
	ExpiresAt string `json:"expires_at"`
}

func jobOf(j store.Job) jobView {
	return jobView{
		ID: j.ID, Session: j.SessionName, Status: j.Status,
		Answer: j.Answer, Error: j.Error,
		ExpiresAt: j.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

// pollInterval is how often a long-poll re-reads the job. Short enough to feel
// immediate, long enough not to spin on the database.
const pollInterval = 250 * time.Millisecond

// getJob reads a job, optionally waiting for it to finish.
//
// respond_within is a query parameter here rather than a body field because
// GET has no body, and it is optional because "tell me the status now" is the
// obvious default for a read — unlike a query, where there is no safe default
// and the caller must say.
func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wait, err := waitParam(r.URL.Query().Get("respond_within"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	deadline := s.now().Add(wait)
	for {
		job, err := s.Store.JobByID(id)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if job == nil {
			fail(w, http.StatusNotFound, fmt.Sprintf("no job %q — it may have expired", id))
			return
		}
		if job.Status != store.Running || !s.now().Before(deadline) {
			writeJSON(w, http.StatusOK, jobOf(*job))
			return
		}
		select {
		case <-r.Context().Done():
			// The caller hung up. The query keeps running; its answer will be
			// waiting in the job.
			return
		case <-time.After(pollInterval):
		}
	}
}

func waitParam(raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	var rw respondWithin
	if err := rw.UnmarshalJSON([]byte(`"` + raw + `"`)); err != nil {
		return 0, err
	}
	return window(&rw)
}

// deleteJob means "I am done with this", in both states a job can be in.
func (s *Server) deleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := s.Store.JobByID(id)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if job == nil {
		// Already gone is the outcome the caller asked for.
		writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
		return
	}
	if job.Status == store.Running {
		// Cancelling costs the turn in flight and nothing else: a SIGKILLed
		// claude -p leaves a transcript --resume continues from, verified
		// against Claude Code 2.1.236.
		s.cancel(id)
		s.Store.FinishJob(id, store.Cancelled, "", "cancelled by the client")
		writeJSON(w, http.StatusOK, map[string]string{"job": id, "status": store.Cancelled})
		return
	}
	if err := s.Store.DeleteJob(id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/vutran1710/claudebox/internal/claude"
	"github.com/vutran1710/claudebox/internal/commandspec"
	"github.com/vutran1710/claudebox/internal/store"
)

// respondWithin is how long the caller will wait for a response.
//
// It is a response deadline, not a cancellation deadline: when it passes the
// query keeps running and the answer lands in a job. Nothing is lost by naming
// a short window — it changes only whether you are told now or told later,
// which is why the field is not called "timeout".
type respondWithin struct {
	d time.Duration
}

// UnmarshalJSON accepts a number of seconds or a Go duration string, so both
// 90 and "5m" do what they obviously mean.
func (rw *respondWithin) UnmarshalJSON(b []byte) error {
	var asNumber float64
	if err := json.Unmarshal(b, &asNumber); err == nil {
		rw.d = time.Duration(asNumber * float64(time.Second))
		return nil
	}
	var asString string
	if err := json.Unmarshal(b, &asString); err != nil {
		return fmt.Errorf("respond_within must be seconds or a duration like \"90s\"")
	}
	if secs, err := strconv.ParseFloat(asString, 64); err == nil {
		rw.d = time.Duration(secs * float64(time.Second))
		return nil
	}
	d, err := time.ParseDuration(asString)
	if err != nil {
		return fmt.Errorf("respond_within %q is not a duration — try 90 or \"5m\"", asString)
	}
	rw.d = d
	return nil
}

type queryRequest struct {
	Prompt string `json:"prompt"`
	// A pointer so an omitted field is distinguishable from zero. Zero is
	// meaningful: it asks for a job id immediately.
	RespondWithin *respondWithin `json:"respond_within"`
}

// window validates the deadline a caller asked for.
func window(rw *respondWithin) (time.Duration, error) {
	if rw == nil {
		return 0, fmt.Errorf("respond_within is required — how long will you wait? seconds, or a duration like \"5m\". Use 0 to get a job id immediately")
	}
	if rw.d < 0 {
		return 0, fmt.Errorf("respond_within cannot be negative")
	}
	if rw.d > MaxRespondWithin {
		// Refused, not clamped. Quietly doing something other than what was
		// asked is the failure this project keeps finding in other tools.
		return 0, fmt.Errorf("respond_within is at most %s", MaxRespondWithin)
	}
	return rw.d, nil
}

func (s *Server) query(w http.ResponseWriter, r *http.Request) {
	sess := s.headless(w, r.PathValue("name"))
	if sess == nil {
		return
	}
	var req queryRequest
	if err := decode(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Prompt == "" {
		fail(w, http.StatusBadRequest, "prompt is required")
		return
	}
	wait, err := window(req.RespondWithin)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.run(w, sess, claude.Request{
		Dir:            sess.Dir,
		Prompt:         req.Prompt,
		SessionID:      sess.ClaudeSessionID,
		SystemPrompt:   sess.SystemPrompt,
		PermissionMode: sess.PermissionMode,
		Model:          sess.Model,
		Effort:         sess.Effort,
		Fresh:          sess.Turns == 0,
	}, wait, req.Prompt)
}

type commandRequest struct {
	Command       string         `json:"command"`
	RespondWithin *respondWithin `json:"respond_within"`
}

func (s *Server) command(w http.ResponseWriter, r *http.Request) {
	sess := s.headless(w, r.PathValue("name"))
	if sess == nil {
		return
	}
	var req commandRequest
	if err := decode(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	spec, err := commandspec.Load(s.SpecPath)
	if err != nil {
		// A malformed spec is refused rather than replaced by the default:
		// answering with a policy nobody chose is worse than answering with
		// an error.
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	cmd, err := spec.Resolve(req.Command)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	if cmd.Effect == commandspec.RotateSession {
		// Forwarded, /clear reports success, forks a new conversation id and
		// leaves the transcript intact. Rotating the id is what actually
		// forgets, so cbx does it rather than asking Claude to.
		sess.ClaudeSessionID = uuid.NewString()
		sess.Turns = 0
		if err := s.Store.Put(*sess); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"command": cmd.Name, "effect": cmd.Effect, "session_id": sess.ClaudeSessionID,
		})
		return
	}

	wait, err := window(req.RespondWithin)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.run(w, sess, claude.Request{
		Dir:            sess.Dir,
		Prompt:         req.Command,
		SessionID:      sess.ClaudeSessionID,
		SystemPrompt:   sess.SystemPrompt,
		PermissionMode: sess.PermissionMode,
		Model:          sess.Model,
		Effort:         sess.Effort,
		Fresh:          sess.Turns == 0,
	}, wait, req.Command)
}

// outcome is what a turn came to. One decision, used for both the job row and
// the HTTP response — computing them separately is how a caller once got a
// 200 for a turn the database had already recorded as failed.
type outcome struct {
	status     string
	answer     string
	errMsg     string
	sessionID  string
	turns      int
	durationMS int
}

// run starts a turn and answers within the caller's window, or hands back a job.
func (s *Server) run(w http.ResponseWriter, sess *store.Session, req claude.Request, wait time.Duration, prompt string) {
	id := s.newID()
	now := s.now()
	err := s.Store.CreateJob(store.Job{
		ID: id, SessionName: sess.Name, Status: store.Running,
		Prompt: prompt, CreatedAt: now, ExpiresAt: now.Add(s.JobTTL),
	})
	if err == store.ErrSessionBusy {
		fail(w, http.StatusConflict, fmt.Sprintf(
			"session %q already has a query running — wait for it, or DELETE its job to cancel", sess.Name))
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Deliberately not the request's context: that is cancelled the moment the
	// handler returns, which for a 202 is while the query is still running.
	// Cancellation comes from DELETE /jobs/{id} instead.
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[id] = cancel
	s.mu.Unlock()

	done := make(chan outcome, 1)
	go func() {
		defer s.forget(id)
		res, err := s.Claude.Query(ctx, req)
		if ctx.Err() != nil {
			// Cancelled. deleteJob already recorded why.
			return
		}
		done <- s.finish(sess, req, id, res, err)
	}()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case o := <-done:
		if o.status != store.Done {
			// 502: the request was fine, the thing behind it was not.
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"job": id, "status": o.status, "error": o.errMsg, "answer": o.answer,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"job": id, "status": o.status, "answer": o.answer,
			"session_id": o.sessionID, "turns": o.turns, "duration_ms": o.durationMS,
		})
	case <-timer.C:
		writeJSON(w, http.StatusAccepted, map[string]any{
			"job": id, "status": store.Running, "poll": "/jobs/" + id,
		})
	}
}

// finish records how a turn ended and reports it, so the row and the response
// can never disagree.
func (s *Server) finish(sess *store.Session, req claude.Request, id string, res *claude.Result, err error) outcome {
	if err != nil {
		o := outcome{status: store.Failed, errMsg: err.Error()}
		s.Store.FinishJob(id, o.status, "", o.errMsg)
		return o
	}
	// A session_id other than the one asked for means the turn ran somewhere
	// else — a locally-handled command forks rather than applying. Measured
	// against Claude Code 2.1.236, and reported so a caller is never told a
	// command worked when it landed on another conversation.
	if res.SessionID != "" && res.SessionID != req.SessionID {
		o := outcome{
			status: store.Failed, answer: res.Answer,
			errMsg: fmt.Sprintf("ran against conversation %s instead of %s — the command did not apply to this session",
				res.SessionID, req.SessionID),
		}
		s.Store.FinishJob(id, o.status, o.answer, o.errMsg)
		return o
	}
	if res.Turns > 0 {
		// Only a turn that reached the model advances the count. A locally
		// handled command spends nothing and creates no conversation, so
		// counting it would make the next query resume one that is not there.
		if fresh, err := s.Store.Get(sess.Name); err == nil && fresh != nil {
			fresh.Turns += res.Turns
			s.Store.Put(*fresh)
		}
	}
	s.Store.FinishJob(id, store.Done, res.Answer, "")
	return outcome{
		status: store.Done, answer: res.Answer,
		sessionID: res.SessionID, turns: res.Turns, durationMS: res.DurationMS,
	}
}

func (s *Server) forget(id string) {
	s.mu.Lock()
	delete(s.cancels, id)
	s.mu.Unlock()
}

func (s *Server) cancel(id string) bool {
	s.mu.Lock()
	cancel, ok := s.cancels[id]
	s.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

func newJobID() string { return "j_" + uuid.NewString() }

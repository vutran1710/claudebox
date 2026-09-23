package api

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vutran1710/claudebox/internal/claude"
	"github.com/vutran1710/claudebox/internal/store"
)

// Running skills into a session as it is created, so a caller can hand a fresh
// conversation its working instructions in one request instead of three.
//
// A skill is invoked the way Claude Code invokes one: as `/name`, a turn of
// its own. That costs real time — a skill took twenty seconds of API time when
// this was measured — so priming follows the same contract as a query. The
// caller says how long it will wait and gets a job if that is not long enough.

// unknownCommand is how Claude Code answers a slash command it does not have.
//
// It reports is_error: false with turns 0, so nothing about the exit code or
// the error flag distinguishes a skill that ran from one that does not exist.
// This string is the only signal, which is why priming looks for it rather
// than trusting success.
const unknownCommand = "Unknown command:"

// skillRef is what a caller may name. Colons are allowed because a skill from
// a plugin is addressed as `plugin:skill`.
var skillRef = regexp.MustCompile(`^[a-z0-9][a-z0-9:_-]{0,63}$`)

// ValidSkillRef reports whether a name is one a caller may invoke.
func ValidSkillRef(name string) bool { return skillRef.MatchString(name) }

type primingView struct {
	Job     string   `json:"job"`
	Status  string   `json:"status"`
	Invoked []string `json:"invoked,omitempty"`
	Error   string   `json:"error,omitempty"`
	Poll    string   `json:"poll,omitempty"`
}

// prime runs each skill as its own turn, in the order given.
//
// It stops at the first skill Claude does not recognise. A session primed with
// half of what was asked for is worse than one that says which half failed:
// the caller's assumption about what the session knows is already wrong, and
// continuing would bury that under later output.
func (s *Server) prime(sess *store.Session, skills []string, wait time.Duration) primingView {
	id := s.newID()
	now := s.now()
	err := s.Store.CreateJob(store.Job{
		ID: id, SessionName: sess.Name, Status: store.Running,
		Prompt:    "prime: " + strings.Join(skills, ", "),
		CreatedAt: now, ExpiresAt: now.Add(s.JobTTL),
	})
	if err != nil {
		return primingView{Status: store.Failed, Error: err.Error()}
	}

	// Not the request's context: for a 201 that hands back a job, the handler
	// returns while the skills are still running.
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[id] = cancel
	s.mu.Unlock()

	done := make(chan primingView, 1)
	go func() {
		defer s.forget(id)
		done <- s.runSkills(ctx, sess, skills, id)
	}()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case got := <-done:
		got.Job = id
		return got
	case <-timer.C:
		return primingView{Job: id, Status: store.Running, Poll: "/jobs/" + id}
	}
}

func (s *Server) runSkills(ctx context.Context, sess *store.Session, skills []string, id string) primingView {
	var invoked []string
	var transcript strings.Builder
	// Read from the row each turn: the conversation is created by the first
	// one, and every turn after it resumes rather than recreating.
	turns := sess.Turns

	for _, name := range skills {
		res, err := s.Claude.Query(ctx, claude.Request{
			Dir:            sess.Dir,
			Prompt:         "/" + name,
			SessionID:      sess.ClaudeSessionID,
			SystemPrompt:   sess.SystemPrompt,
			PermissionMode: sess.PermissionMode,
			Model:          sess.Model,
			Effort:         sess.Effort,
			Fresh:          turns == 0,
		})
		if ctx.Err() != nil {
			return primingView{Status: store.Cancelled, Invoked: invoked}
		}
		if err != nil {
			s.Store.FinishJob(id, store.Failed, transcript.String(), err.Error())
			return primingView{Status: store.Failed, Invoked: invoked, Error: err.Error()}
		}
		// The measured trap: an unknown skill answers with this and reports
		// success. Without the check, a session would be created claiming to
		// know something nothing ever taught it.
		if strings.HasPrefix(strings.TrimSpace(res.Answer), unknownCommand) {
			msg := fmt.Sprintf("skill %q is not installed on this box — `cbx export skills` lists what is", name)
			s.Store.FinishJob(id, store.Failed, transcript.String(), msg)
			return primingView{Status: store.Failed, Invoked: invoked, Error: msg}
		}
		if res.SessionID != "" && res.SessionID != sess.ClaudeSessionID {
			msg := fmt.Sprintf("skill %q ran against conversation %s instead of %s", name, res.SessionID, sess.ClaudeSessionID)
			s.Store.FinishJob(id, store.Failed, transcript.String(), msg)
			return primingView{Status: store.Failed, Invoked: invoked, Error: msg}
		}

		turns += res.Turns
		invoked = append(invoked, name)
		fmt.Fprintf(&transcript, "## /%s\n\n%s\n\n", name, res.Answer)
	}

	if turns != sess.Turns {
		if fresh, err := s.Store.Get(sess.Name); err == nil && fresh != nil {
			fresh.Turns = turns
			s.Store.Put(*fresh)
		}
	}
	s.Store.FinishJob(id, store.Done, transcript.String(), "")
	return primingView{Status: store.Done, Invoked: invoked}
}

package store

import (
	"maps"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPutThenGetRoundTrips(t *testing.T) {
	s := open(t)
	want := Session{Name: "master", Dir: "/workspace/master", Repo: "owner/repo",
		RCURL: "https://claude.ai/code/session_x", CreatedAt: time.Unix(1700000000, 0),
		Env: map[string]string{"TENANT_ID": "12"}}
	if err := s.Put(want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get("master")
	if err != nil || got == nil {
		t.Fatalf("Get: %v, %v", got, err)
	}
	if got.Dir != want.Dir || got.Repo != want.Repo || got.RCURL != want.RCURL || !maps.Equal(got.Env, want.Env) {
		t.Errorf("got %+v, want %+v", *got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

// The Remote Control URL is the whole reason this store exists: restarting the
// tmux server once lost a live session's URL because nothing had recorded it.
func TestGetSurvivesWhatTmuxWouldForget(t *testing.T) {
	s := open(t)
	s.Put(Session{Name: "master", Dir: "/w/master", RCURL: "https://claude.ai/code/session_keepme"})
	got, _ := s.Get("master")
	if got == nil || got.RCURL != "https://claude.ai/code/session_keepme" {
		t.Fatalf("RC URL not durable: %+v", got)
	}
}

func TestGetMissingIsNotAnError(t *testing.T) {
	s := open(t)
	got, err := s.Get("nope")
	if err != nil {
		t.Errorf("err = %v, want nil for an absent session", err)
	}
	if got != nil {
		t.Errorf("got = %+v, want nil", got)
	}
}

// A killed name gets reused; the new row must win rather than collide.
func TestPutReplacesAnEarlierRowWithTheSameName(t *testing.T) {
	s := open(t)
	s.Put(Session{Name: "app", Dir: "/old", RCURL: "old"})
	if err := s.Put(Session{Name: "app", Dir: "/new", RCURL: "new"}); err != nil {
		t.Fatalf("second Put: %v", err)
	}
	got, _ := s.Get("app")
	if got.Dir != "/new" || got.RCURL != "new" {
		t.Errorf("got %+v, want the second row", *got)
	}
	all, _ := s.List()
	if len(all) != 1 {
		t.Errorf("List = %d rows, want 1", len(all))
	}
}

func TestListIsOldestFirst(t *testing.T) {
	s := open(t)
	s.Put(Session{Name: "second", Dir: "/b", CreatedAt: time.Unix(2000, 0)})
	s.Put(Session{Name: "first", Dir: "/a", CreatedAt: time.Unix(1000, 0)})
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].Name != "first" || got[1].Name != "second" {
		t.Errorf("List order = %v", []string{got[0].Name, got[1].Name})
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	s := open(t)
	s.Put(Session{Name: "app", Dir: "/a"})
	if err := s.Delete("app"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, _ := s.Get("app"); got != nil {
		t.Error("still present after Delete")
	}
	if err := s.Delete("app"); err != nil {
		t.Errorf("second Delete: %v — deleting an absent session should succeed", err)
	}
}

func TestPutRejectsAnEmptyName(t *testing.T) {
	if err := open(t).Put(Session{Dir: "/a"}); err == nil {
		t.Error("expected an error for an empty session name")
	}
}

// The master session and a human over SSH can run cbx at the same time. A
// read-modify-write over a JSON file loses one of them; this is the reason
// the store is SQLite.
func TestConcurrentPutsDoNotLoseUpdates(t *testing.T) {
	s := open(t)
	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Put(Session{Name: string(rune('a'+i)) + "-sess", Dir: "/w"}); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Put: %v", err)
	}
	got, _ := s.List()
	if len(got) != n {
		t.Errorf("List = %d sessions, want %d — writes were lost", len(got), n)
	}
}

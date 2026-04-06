package session

import (
	"testing"
)

func TestTmuxManagerImplementsInterface(t *testing.T) {
	// Compile-time check that TmuxManager implements Manager
	var _ Manager = (*TmuxManager)(nil)
}

func TestNewTmuxManager(t *testing.T) {
	mgr := NewTmuxManager()
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestCreateRequiresName(t *testing.T) {
	mgr := NewTmuxManager()
	_, err := mgr.Create("", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestIsRunning_NonexistentSession(t *testing.T) {
	mgr := NewTmuxManager()
	if mgr.IsRunning("nonexistent-session-xyz-12345") {
		t.Error("expected nonexistent session to not be running")
	}
}

func TestList_Empty(t *testing.T) {
	// This may return sessions if tmux is running with other sessions
	// Just verify it doesn't error
	mgr := NewTmuxManager()
	_, err := mgr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

package session

// Session represents a Claude Code tmux session.
type Session struct {
	Name   string `json:"name"`
	Dir    string `json:"dir,omitempty"`
	Status string `json:"status"` // running, stopped
	RCURL  string `json:"remote_control,omitempty"`
}

// Manager manages Claude Code sessions.
type Manager interface {
	Create(name, workDir string) (*Session, error)
	List() ([]Session, error)
	Kill(name string) error
	IsRunning(name string) bool
}

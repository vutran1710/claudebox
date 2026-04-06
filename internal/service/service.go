package service

// Status represents the state of a managed service.
type Status struct {
	Name      string            `json:"name"`
	Running   bool              `json:"running"`
	Port      int               `json:"port,omitempty"`
	TunnelURL string            `json:"tunnel_url,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// Service manages a long-running external service.
type Service interface {
	Name() string
	Start() error
	Stop()
	Status() Status
}

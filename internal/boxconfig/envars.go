package boxconfig

// envars.yaml: named env entries a session can take whole, such as one per deployment
// of a caller that reaches this box from several places. Its own file, beside cbx.yaml,
// because the API writes it: a write here can never touch the roles.

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// Envars maps an entry's name to the variables it sets.
type Envars map[string]map[string]string

// DefaultEnvarsPath is where the entries live, beside cbx.yaml.
func DefaultEnvarsPath() string { return filepath.Join(configDir(), "envars.yaml") }

// envarsMu orders the read-modify-write of concurrent PUTs and DELETEs, so two deploys
// writing at once both land.
var envarsMu sync.Mutex

// LoadEnvars reads the entries. An absent file is none; a malformed one is an error
// naming the file, never an empty map, which would refuse every session that names one.
func LoadEnvars(path string) (Envars, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Envars{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	envs := Envars{}
	if err := yaml.Unmarshal(raw, &envs); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return envs, nil
}

// PutEnvar adds or replaces one entry. A malformed file is refused rather than
// overwritten, so the entries a person can still read in it are not lost.
func PutEnvar(path, name string, vars map[string]string) error {
	return editEnvars(path, func(envs Envars) bool { envs[name] = vars; return true })
}

// DeleteEnvar removes one entry, reporting whether it was there.
func DeleteEnvar(path, name string) (bool, error) {
	var found bool
	err := editEnvars(path, func(envs Envars) bool {
		_, found = envs[name]
		delete(envs, name)
		return found
	})
	return found, err
}

func editEnvars(path string, edit func(Envars) bool) error {
	envarsMu.Lock()
	defer envarsMu.Unlock()
	envs, err := LoadEnvars(path)
	if err != nil {
		return err
	}
	if !edit(envs) {
		return nil
	}
	out, err := yaml.Marshal(envs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

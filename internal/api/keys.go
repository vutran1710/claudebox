package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/vutran1710/claudebox/internal/boxconfig"
	"github.com/vutran1710/claudebox/internal/claude"
	"github.com/vutran1710/claudebox/internal/store"
)

// Who is calling, and what their sessions may do.
//
// Permissions belong to the key, not to a request. A caller able to name its
// own permission mode could name the one that ignores every rule — measured:
// under bypassPermissions a deny rule is not consulted at all. So the config
// file decides, the caller presents a key, and the two are never mixed.

// presentedKey pulls the key out of a request's Authorization header.
//
// The only thing left of what used to be a parallel key store here. Keys live
// in the config file and nowhere else: two places to look for "what is the
// current key" is one more than can be kept true.
func presentedKey(header string) string {
	const bearer = "Bearer "
	if len(header) > len(bearer) && strings.EqualFold(header[:len(bearer)], bearer) {
		return strings.TrimSpace(header[len(bearer):])
	}
	return ""
}

type callerKey struct{}

// permit is what the caller of this request is allowed to do: the role its key
// names, already resolved.
type permit struct {
	Label string
	Role  *boxconfig.Role
}

// caller returns the permissions a request authenticated with.
func caller(r *http.Request) *permit {
	p, _ := r.Context().Value(callerKey{}).(*permit)
	return p
}

// role is this caller's role, or none at all for a caller without one.
func (p *permit) role() boxconfig.Role {
	if p == nil || p.Role == nil {
		return boxconfig.Role{}
	}
	return *p.Role
}

// Mode is the Claude Code permission mode sessions run under.
//
// The same for every caller. A role governs what a session may touch, through
// its deny rules; the mode is how Claude handles everything else, and only
// acceptEdits both works unattended and honours those rules.
func (p *permit) Mode() string { return claude.AcceptEdits }

// authenticate resolves the presented key against the config, and carries the
// entry it matched through to the handlers.
//
// The config is read per request, the same as the command allowlist, so
// revoking a key is an edit rather than a restart.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented := presentedKey(r.Header.Get("Authorization"))
		if presented == "" {
			fail(w, http.StatusUnauthorized, "a valid Authorization: Bearer <key> is required")
			return
		}
		key, err := s.Store.KeyByValue(presented)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if key == nil {
			fail(w, http.StatusUnauthorized, "that key is not registered on this box")
			return
		}
		cfg, err := boxconfig.Load(s.ConfigPath)
		if err != nil {
			// A config that does not parse authorises nobody. Failing open
			// would mean a typo in the policy file quietly removed the policy.
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		role, err := cfg.Role(key.Permission)
		if err != nil {
			// The key is real and its permissions are not. Refusing is the
			// only safe reading: the alternative is guessing what somebody
			// meant it to be allowed to do.
			fail(w, http.StatusForbidden, fmt.Sprintf("key %q: %v", key.Label, err))
			return
		}
		ctx := context.WithValue(r.Context(), callerKey{}, &permit{Label: key.Label, Role: role})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// settingsFor writes the calling key's deny rules where Claude Code can read
// them, and returns the path.
//
// Written per key rather than per session, and rewritten each time so an edit
// to the config takes effect on the next query. Empty when a key denies
// nothing, so no --settings flag is passed at all.
func (s *Server) settingsFor(p *permit) (string, error) {
	if p == nil || p.Role == nil || len(p.Role.Deny) == 0 {
		return "", nil
	}
	dir := filepath.Join(stateDir(), "permissions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	body, err := json.Marshal(map[string]any{
		"permissions": map[string]any{"deny": p.Role.Deny},
	})
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, p.Label+".json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("write permissions for key %q: %w", p.Label, err)
	}
	return path, nil
}

// legacyKeyPath is where a single key used to live, before the config listed
// them. Read once on startup and then removed.
func legacyKeyPath() string { return filepath.Join(stateDir(), "api-key") }

// adoptLegacyKey moves a box's previous key into the config, once.
//
// A box provisioned before keys were listed has a value its callers are
// already using; dropping it would lock them out at the moment nobody expected
// a change. The old file is deleted afterwards, because a key readable from
// two places is a question with two answers.
func adoptLegacyKey(st *store.Store) (bool, error) {
	raw, err := os.ReadFile(legacyKeyPath())
	if err != nil {
		return false, err
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return false, nil
	}
	adopted, err := st.AdoptKey("migrated", value, "migrated")
	if err != nil {
		return false, err
	}
	if !adopted {
		return false, os.Remove(legacyKeyPath())
	}
	return true, os.Remove(legacyKeyPath())
}

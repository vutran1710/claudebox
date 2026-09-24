package setuptool

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/vutran1710/claudebox/internal/boxconfig"
)

// Installing and operating the API server on a box.
//
// The unit does the supervising — restart on failure, start on boot, and
// killing query children with the service. `cbx serve --detach` exists for a
// machine without systemd; a box provisioned by this tool has it, so this uses
// it.

//go:embed templates/cbx-api.service
var unitTemplate string

// UnitPath is where the service definition lives on the box.
const UnitPath = "/etc/systemd/system/cbx-api.service"

// APIOptions are what the API step was asked for.
type APIOptions struct {
	// Addr the server binds. Loopback by default: over plain HTTP a public
	// listener puts the bearer key and every prompt on the wire in cleartext.
	Addr string
}

// DefaultAPIAddr matches what `cbx serve` binds on its own.
const DefaultAPIAddr = "127.0.0.1:8091"

// InstallAPI writes the unit, enables it, and starts the server.
func InstallAPI(t Target, opts APIOptions) error {
	if opts.Addr == "" {
		opts.Addr = DefaultAPIAddr
	}
	home, err := remoteHomeDir(t)
	if err != nil {
		return err
	}
	tmpl, err := template.New("unit").Parse(unitTemplate)
	if err != nil {
		return err
	}
	var unit bytes.Buffer
	if err := tmpl.Execute(&unit, struct{ Addr, User, Home string }{opts.Addr, t.User, home}); err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "cbx-api-*.service")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(unit.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	// Via /tmp: scp authenticates as the ssh user, who cannot write
	// /etc/systemd/system. The install is the privileged half.
	if err := Upload(t, tmp.Name(), "/tmp/cbx-api.service"); err != nil {
		return err
	}
	if _, err := remoteRoot(t, "install -m 0644 /tmp/cbx-api.service "+shq(UnitPath)+" && rm -f /tmp/cbx-api.service"); err != nil {
		return fmt.Errorf("write the service unit: %w", err)
	}
	if _, err := remoteRoot(t, "systemctl daemon-reload && systemctl enable --now cbx-api"); err != nil {
		return fmt.Errorf("start the API service: %w", err)
	}
	return nil
}

// UploadCommandSpec puts the default allowlist on the box.
//
// Shipped rather than left to the binary's own fallback so the file is on
// disk, where it can be read and edited. A spec nobody can see is a policy
// nobody can review.
func UploadCommandSpec(t Target) error {
	home, err := remoteHomeDir(t)
	if err != nil {
		return err
	}
	dest := ConfigPath(home)
	if _, err := Run(t, "mkdir -p "+shq(home+"/.config/cbx")); err != nil {
		return err
	}
	// Never overwrite an edited config. The operator's policy outranks ours —
	// and that includes a box still holding the pre-roles filename, where
	// writing cbx.yaml would silently retire the file they edited.
	legacy := LegacyConfigPath(home)
	if _, err := Run(t, "test -f "+shq(dest)+" -o -f "+shq(legacy)); err == nil {
		return nil
	}
	tmp, err := os.CreateTemp("", "cbx-commands-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(boxconfig.Default()); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	return Upload(t, tmp.Name(), dest)
}

// APIRunning reports whether the service is up.
func APIRunning(t Target) bool {
	_, err := Run(t, "systemctl is-active --quiet cbx-api")
	return err == nil
}

// Keys drives `cbx api-key` on the box.
//
// A pass-through rather than a second implementation: the box already knows
// how to manage its own keys, and having setuptool know too would be two
// answers to what a key is. It reaches them over ssh, which is why setuptool
// needs no key of its own — ssh already outranks any of them.
//
// A key is issued against a *role*, and the role decides what the sessions
// created with it may touch. The roles themselves live in the box's config
// file and are edited there.
func Keys(t Target, action, label, role string) (string, error) {
	cmd := "cbx api-key " + shq(action)
	if label != "" {
		cmd += " " + shq(label)
	}
	if role != "" {
		cmd += " --role " + shq(role)
	}
	out, err := remote(t, cmd)
	if err != nil {
		return "", fmt.Errorf("api-key %s: %w", action, err)
	}
	return strings.TrimRight(out, "\n"), nil
}

// ConfigName is what the box's policy file is called.
//
// One definition because two places name it: the uploader writes it and setup
// prints it. They drifted once — written as cbx.yaml, reported as the
// pre-roles commands.yaml — which is a tool lying about what it just did.
const ConfigName = "cbx.yaml"

// LegacyConfigName is what the file was called before it held roles as well
// as commands. Read so a box configured earlier is left alone rather than
// quietly given a second config the server would prefer.
const LegacyConfigName = "commands.yaml"

// ConfigPath is where the config lives in a given home directory.
func ConfigPath(home string) string { return home + "/.config/cbx/" + ConfigName }

// LegacyConfigPath is the same, under the pre-roles name.
func LegacyConfigPath(home string) string { return home + "/.config/cbx/" + LegacyConfigName }

// DefaultRole is what a key gets when nobody says. Least privilege that still
// works: a session can write its own output and nothing that shapes the box.
// An unrestricted default would make the role system opt-in.
const DefaultRole = "reporter"

// APIKey reads a usable key off the box, issuing one if it has none.
//
// A box with no keys is not a failure — it is the ordinary state of a box
// being set up for the first time, and the API is unusable until one exists.
// Refusing here made `setup --with-api` fail on exactly the machine it was
// meant to prepare.
func APIKey(t Target) (string, error) {
	out, err := remote(t, "cbx api-key list")
	if err != nil {
		return "", fmt.Errorf("read the API keys: %w", err)
	}
	if key, ok := firstKey(out); ok {
		return key, nil
	}
	issued, err := Keys(t, "add", "setup", DefaultRole)
	if err != nil {
		return "", err
	}
	// `api-key add` reports the whole record — label, role, value — so the
	// value has to be picked out. Returning the record printed three lines
	// into a field sized for one.
	key, ok := firstKey(issued)
	if !ok {
		return "", fmt.Errorf("issued a key but could not read it back from: %s", strings.TrimSpace(issued))
	}
	return key, nil
}

// firstKey picks a usable key out of `cbx api-key list`. The second return
// distinguishes "none yet" from a parse that went wrong, because the first is
// ordinary and the second is not.
func firstKey(listing string) (string, bool) {
	for _, line := range strings.Split(listing, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) >= 2 && strings.HasPrefix(fields[1], "cbx_live_") {
			return fields[1], true
		}
	}
	return "", false
}

// parseFact pulls one value out of cbx's tab-separated output.
func parseFact(out, name string) (string, error) {
	for _, line := range strings.Split(out, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "\t")
		if found && key == name {
			return value, nil
		}
	}
	return "", fmt.Errorf("no %q in the output: %s", name, strings.TrimSpace(out))
}

// ForwardCommand is how to reach a loopback-bound API from a laptop.
//
// An ssh tunnel rather than opening the port: it needs nothing installed on
// the box, and it encrypts what a plain HTTP listener would not.
func ForwardCommand(t Target, addr string) string {
	_, port, found := strings.Cut(addr, ":")
	if !found {
		port = "8091"
	}
	return fmt.Sprintf("ssh -N -L %s:localhost:%s %s", port, port, t)
}

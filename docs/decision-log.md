# Decision log

Choices made while building the two-binary redesign, recorded so they can be
reviewed or reversed. Newest last. Each entry says what was decided, why, and
what would make it wrong.

## SQLite driver: `modernc.org/sqlite`

Pure Go, so `CGO_ENABLED=0 GOOS=linux` cross-compiles from a Mac — which is how
release binaries are built. A cgo driver (`mattn/go-sqlite3`) would break that.

Validated before committing: built and ran a scratch program natively, then
cross-compiled it to linux/amd64. Costs ~7MB of binary. Acceptable for
something cloud-init downloads once.

**Wrong if** the binary size becomes a problem, or the box ever needs a driver
feature modernc lacks.

## SQLite pragmas go in the DSN, not an `Exec`

`PRAGMA busy_timeout` set via `db.Exec` applies only to whichever pooled
connection served it. `database/sql` opens others on demand and they get
defaults, so concurrent writers still fail instantly.

Found by test, not by reading: the concurrency test lost 19 of 20 writes to
`SQLITE_BUSY`. Moving the pragmas into the DSN fixed it, and reverting them in
a scratch worktree reproduced the failure — so the test discriminates.

## Session database lives under `XDG_STATE_HOME`

`~/.local/state/cbx/sessions.db`, not `~/.config`. It is generated data cbx can
rebuild by reconciling against tmux, which is state rather than configuration.

## `tmux.Client.Start` refuses to replace a running session

The old code killed any existing session of the same name first. That silently
destroys work in progress. Reattaching is `resume`'s job; if you really want it
gone, `kill` says so explicitly.

## The tmux command is injectable

`Client.WithCommand` exists so tests drive real tmux with something cheap
instead of Claude Code. This is what makes `cbx` fully testable on a laptop,
which was a stated requirement — the package has no remote dependency at all.

## `Kill` is idempotent, `Get` on a missing session is not an error

Both are things an agent asks about routinely. Returning an error for "already
gone" or "not found" makes callers write error-handling for the normal case.

## `--dangerously-skip-permissions` stays the default, but is named

An automated security review flagged it as HIGH. Keeping it: these sessions are
driven from a phone with nobody at a terminal, so a permission prompt has no
one to answer and the session just stalls. That is the product, and the box is
single-tenant and owned by whoever ran cbx.

Taking the reviewer's real point though — it was hidden inside a constructor.
It is now a named constant with the reasoning attached, and
`WithPermissionPrompts()` turns it off. A deliberate risk should be visible at
the point it is taken.

**Wrong if** ClaudeBox ever hosts sessions for someone other than the box's
owner, at which point the blast radius stops being self-inflicted.

## Integration and smoke tests are required, not optional

Standing instruction: every part of this must be validated by integration tests
locally and a smoke test against a real droplet before it counts as done. The
last large refactor passed 181 unit tests and a mutation-tested review, then
forty minutes on a real box found six defects — three blocking — because every
test used a fake and fakes cannot model a shell, a PATH, or an installer that
exits 0 having done nothing.

Droplets may be created freely for this. **Every droplet except `claudebox`
must be destroyed when the work is finished.**

## Commit hygiene: stage explicit paths

`git add -A` twice swept a subagent's or my own in-flight work into an
unrelated commit — once putting 209 lines of Go inside a commit labelled
`docs:`. Both were caught and rewritten before pushing. The rule now is
`git commit --only <paths>`, which commits the named paths regardless of what
else is staged.

## Removed `internal/shell`'s PATH-clobbering `init()`

Not a planned change — it blocked the work. A new integration test kept
skipping with "tmux not installed" on a machine where tmux is installed. The
cause: `shell.init()` ran `os.Setenv("PATH", FullPATH)`, so importing the
package anywhere in a binary replaced the PATH of the whole process with a
hardcoded Linux list. `/opt/homebrew/bin` vanished and with it tmux and gh. The
test package never imported `shell` directly; it arrived transitively.

The `init()` was redundant besides — `RunShell` already sets the PATH per
command. It only affected `exec.LookPath`, so `Which` now resolves against
`FullPATH` explicitly instead.

An AST test asserts the package declares no `init()`, because this is invisible
until something far away breaks.

**Note for the eventual split:** `FullPATH` still hardcodes `/root/...`, which
is wrong for any non-root user. `internal/tmux` takes the other approach —
prepending `$HOME/.local/bin` inside the script so the shell expands it. The
`FullPATH` constant should follow when the setup tool moves.

## Three Remote-Control bugs, all found by running the binary

None of these were caught by tests. Each produced a session that started fine
and then sat for a 60-second timeout with no URL. Found by running `cbx new` on
a Mac and reading the tmux pane.

1. **Claude asks to trust the folder.** `cbx new` creates the project
   directory, so Claude has never seen it and asks every single time.
   `/remote-control` typed into that prompt does nothing.
2. **Text and Enter cannot go in one `send-keys`.** Claude receives the Enter
   before it has registered `/remote-control` as a slash command, treats the
   whole thing as a prompt, and helpfully searches the filesystem for a command
   definition. They now go separately with a pause.
3. **Asking once is not enough.** The first version captured the pane
   immediately, saw a session still booting, found no prompt to answer, fired
   the command into nothing, and marked itself done. It now waits for Claude's
   input line to appear and re-asks every 15 seconds.

Bug 3 was briefly hidden by my own debug harness, which slept before its first
capture and so never reproduced the race. Worth remembering: an instrument that
does not match the code path can exonerate a bug.

All three now have regression tests, including one asserting the command and
the Enter are separate send-keys.

## `cbx` output format: tab-separated, one fact per line

`name\tvalue` for single results, `name\tstatus\tdir\turl` for lists. Chosen
over JSON because the caller is an agent with a shell: `cut -f2` and
`awk -F'\t'` work without a parser, and the format is readable when a person
looks at it. JSON is easy to add later as a flag and hard to remove.

## Shell-quoting is not argv-safety

`git clone` was built as a shell string with both arguments `shellQuote`d. That
is shell-safe and still wrong: quoting makes a value *one argv element*, and
that element can still be an option. `git clone --upload-pack=<cmd>` executes
`<cmd>`, so `cbx new x --repo '--upload-pack=touch /tmp/pwned'` was remote code
execution with perfectly correct quoting.

This is the third time the same distinction has bitten in this project — the
first was a host beginning with `-` reaching `ssh` as `-oProxyCommand=`, the
second a path reaching `scp`. Quoting defends the shell; it does nothing about
the program's own option parser.

The clone now goes through `exec.Command("git", "clone", "--", url, dir)` with
no shell at all, plus a validator that rejects a leading dash and requires
shorthand to look like `owner/repo`. Tests cover both the parser and the
end-to-end case, including that a rejected clone leaves no directory behind.

**Rule for the rest of this work:** if a value reaches a program that parses
options, either pass it after `--` via `exec.Command`, or reject a leading
dash. Quoting alone is not an answer.

## Tool tokens arrive on stdin, never in argv

`gh`, `vercel` and `supabase` are authenticated by piping a token over SSH into
the tool's login. A token passed as a command-line argument is visible in the
box's process table to every other user for as long as the command runs, and
lands in shell history.

The three tools disagree about how to accept one, which is exactly why the
recipes live in one table rather than scattered through provisioning:

- `gh auth login --with-token` reads stdin. Straightforward.
- `supabase login --token "$(cat)"` — the substitution happens in the remote
  shell, so the token is still never in cbx's argv.
- `vercel` has no stdin login at all; it reads `VERCEL_TOKEN` or `--token`.
  Its config file is written directly instead, with `umask 077` so it is
  private from creation rather than chmod-ed after a brief world-readable
  window. `printf` is a shell builtin, so even there the token never becomes
  the argv of a forked process.

Every recipe is followed by a verify command. Without one a bad or expired
token looks exactly like success, and the failure surfaces later inside a
session with no obvious cause.

Claude Code is deliberately absent from this list: subscription login is an
interactive browser OAuth with no token path. A test asserts it never appears
there, so nobody later "adds it for consistency".

Tokens are also read from the local environment (`GH_TOKEN`, `VERCEL_TOKEN`,
`SUPABASE_ACCESS_TOKEN`) so someone who already has them exported is not asked
to paste anything.

## setuptool prints progress rather than rendering a full-screen TUI

The brief said interactive, and it is — it prompts, pastes, and shows progress.
But it is not a Bubble Tea program that owns the screen, because the login step
hands the terminal to a nested Claude Code. A full-screen program has to be
suspended and restored around that, which is a lot of machinery for a flow a
person watches once per box. Line-by-line progress with `✓ · ✗` gives the same
information and composes with the interactive step instead of fighting it.

**Wrong if** setup grows steps that run concurrently or need live updating, at
which point a real TUI starts earning its complexity.

## Every install step is re-checked after it runs

`Step.Run` runs `Check` again after `Do`, whatever `Do`'s exit code was. A
`curl | bash` that exits 0 having installed nothing printed "✓ Supabase CLI" on
a real droplet where the binary existed nowhere on the filesystem.

The reverse is also handled: a `Do` that errors but whose `Check` then passes
is a success, because installers routinely exit non-zero on a harmless warning.

The supabase step was rewritten as a result — its official install script drops
a binary in the working directory rather than onto PATH, which is why it
appeared to succeed and left nothing behind.

## `InstallCBX` checks the ELF magic before uploading

Uploading a darwin build to a linux box fails later with "cannot execute binary
file", at a point far from the cause. Four bytes of magic turn that into an
error that names the fix.

## `cbx new` diagnoses a missing login instead of timing out

On a box where Claude Code is not signed in, a session starts fine and then
never produces a Remote Control URL. The first version waited the full 60
seconds and reported "no URL appeared", which is true and useless.

It now checks `claude auth status` first and says so in 0.9 seconds, naming
`cbx-setuptool setup` as the fix. The session still starts and still exits 0 —
it is usable over `tmux attach`, and failing the command would leave an agent
unsure whether to retry.

Found by running `cbx new` on a freshly provisioned box before signing in,
which is exactly the state a real first-run is in.

## Dropped `sqlite3` from the installed packages

It was in the apt list but not in the step's `Check`, so on a box that already
had tmux and git the step skipped and sqlite3 never arrived. Same shape as the
installer that lies: a Check that does not cover what its Do claims.

Removed rather than added to the Check, because nothing needs it —
`modernc.org/sqlite` is pure Go, and `cbx export db` exists precisely so a
person never has to open the database by hand.

## Claude Code refuses `--dangerously-skip-permissions` as root, and that explains the `claude` user

The smoke test found this, and it overturns an assumption:

    --dangerously-skip-permissions cannot be used with root/sudo privileges
    for security reasons

The session exits immediately, its tmux window closes, and `cbx new` appears to
succeed while leaving nothing behind. `cbx ls` then reports `stopped` a second
later.

So the original `claude` user was not a security preference — it was a
workaround for a constraint Claude Code imposes. Nine bug-fix commits went into
bridging root-installed tools to that account, and every one of them was the
cost of this single check.

**`IS_SANDBOX=1` is the sanctioned escape.** With it, a root session starts and
stays up. That is a true statement here: a ClaudeBox droplet is single-tenant
and exists only to run these sessions. Setting it means "no identity switching"
survives as a design rule rather than being quietly reversed.

Verified on a droplet: without it the session dies within two seconds, with it
`cbx ls` reports it running.

**Wrong if** ClaudeBox ever runs sessions for anyone but the box's owner, or on
a machine doing anything else — at which point the sandbox claim stops being
true and a separate user becomes honest rather than ceremonial.

## Three PATH findings from the smoke test

All three were invisible locally and to `cbx-setuptool status`, because that
command prepends the tool PATH itself before checking.

1. **npm's global prefix was `$HOME/.npm-global`**, so `vercel` was invisible to
   `ssh box vercel ...`. Now `/usr/local`, which is on the default PATH — the
   right answer for a root-owned single-purpose box.
2. **`claude` installs to `~/.local/bin`** and is now symlinked into
   `/usr/local/bin`, so nothing has to know where the installer put it.
3. **`sqlite3` was listed for install but not covered by its step's `Check`**,
   so on a box that already had tmux and git the step skipped and it never
   arrived. Dropped rather than fixed: nothing needs it.

## The portability pass was lost in the rewrite and had to be re-ported

`MigrateConfig` initially uploaded `settings.json` verbatim, reintroducing a bug
an earlier droplet had already found: hooks referencing `/Users/...` and
binaries the box lacks, firing on every edit inside every session.

Re-ported with the lessons intact — compound commands are split into segments
so `cat >/dev/null || true; ... && code-review-graph ...` is understood,
builtins never cause a drop, and the trailing boundary accepts a quote so
`--repo "/Users/you"` is rewritten. Tested against the real file's shape rather
than a simplified fixture, which is how the first version passed while being
wrong.

## A step's Check must test what the rest of the world sees

Third instance of one mistake, and the most expensive: `Check` used the tool
PATH (`$HOME/.local/bin` prepended), so it passed on the strength of a prefix
that `ssh box <bin>` does not set. The step was skipped, and the binary stayed
invisible to everything except cbx itself.

`onDefaultPath` now exists alongside `has` for exactly this: any step claiming
to put something on the PATH verifies it *without* the prefix.

The same error appeared inside a step's own script. The claude installer's Do
ended with `command -v claude`, which found the binary under the tool PATH and
returned success even when the symlink onto `/usr/local/bin` had never been
made. It now locates the binary, links it, and tests the link.

**Process note:** two of my scripted edits to this file failed silently —
`str.replace` with a stale anchor is a no-op, and I rebuilt and re-tested
unchanged code twice before noticing. Edits now assert their anchor exists
before writing.

## v0.8.0 shipped, droplets cleaned up

Two binaries, eight release artifacts. Only `claudebox` (178.128.118.33)
remains — every droplet created for validation was destroyed, and no orphaned
firewalls were left behind.

`claudebox` is still running the **old** cbx from v0.7.0. It has the manual
`usermod -aG docker claude` fix applied by hand and still uses the dedicated
`claude` user, so `tmux ls` and `claude auth status` as root still report an
empty, signed-out box that is neither. Re-provisioning it with the new
`cbx-setuptool` would retire both problems, but it is a live box with a session
on it, so that is the owner's call rather than something to do unattended.

## What was not verified

The Claude sign-in is the one step that needs a person: subscription login is a
browser OAuth with no token path. Every smoke run used `--skip-claude-login`,
so what is proven end to end is provisioning, config migration, and the full
session lifecycle on a box where Claude is *not* signed in — `cbx new` there
correctly starts a session and reports the missing login in under a second.

Unverified: that a session on a signed-in box produces a Remote Control URL
reachable from the phone. The mechanism is exercised locally against a
signed-in Mac, where it produces a working URL in ~9 seconds, so the remaining
risk is specific to a remote box rather than to the code path.

Token auth for gh/vercel/supabase is likewise unexercised against real tokens —
the recipes and their verify commands are unit-tested, but no live token was
used.

## Standing permission (owner, explicit)

Test droplets may be created freely. `claudebox` (178.128.118.33) is the
owner's live box and is never to be modified or destroyed.

## Smoke-test gaps closed

The first version covered install and the basic lifecycle. Five things it never
touched, now added and passing from a clean droplet:

- **`cbx new --repo`** — the clone path was entirely untested against a real
  box. It works, and it correctly refuses to clone into a non-empty directory.
- **A hostile `--repo`** — `--upload-pack=touch /tmp/pwned` is refused, runs no
  payload, and leaves no directory behind. Previously only unit-tested.
- **`cbx export skills` and `rules`** — only `db` was covered.
- **Twelve concurrent `cbx new`, from separate processes.** All twelve
  recorded, no lost writes. This is the reason the store is SQLite rather than
  a JSON file, and it had only ever been proven with goroutines inside one
  process, which is a much weaker claim.
- **A bad token is caught, not reported as success.** Tested with a deliberately
  invalid `GH_TOKEN`: the login fails with GitHub's own 401, the tool reports
  it, and `status` still shows the box unauthenticated. No real credential was
  put on a test box to prove this.

Two of the failures during this work were my assertions rather than the code:
counting all sessions instead of the concurrent ones, and forgetting that
`cbx kill` deliberately does not delete a project directory.

## Still unverified, and why

A session on a **signed-in** box producing a Remote Control URL. Every smoke
run uses `--skip-claude-login`, because subscription sign-in is a browser OAuth
that needs a person. The mechanism is exercised locally against a signed-in Mac
where it produces a working URL in about nine seconds, so what remains untested
is that specific path on a remote box, not the code.

Real tokens for gh/vercel/supabase. The plumbing and the rejection path are
proven; a successful login with a valid token is not.

## `migrate` follows symlinks, reversing a deliberate skip

`uploadDir` skipped anything that was not a regular file, with the reasoning
that "a symlink under skills/ would otherwise be dereferenced and ship the
contents of whatever it points at — skills come from marketplaces". That is a
sound worry and it produced the opposite failure.

Run against a real configuration directory, `agents/` sent **zero** files and
printed a tick. Its four entries were symlinks into a dotfiles repository,
which is how a lot of people keep this directory. The concern was about
shipping content nobody asked for; the behaviour was shipping nothing while
claiming otherwise — the same shape as the installer that exits 0 having done
nothing, which `Step.Run` already exists to catch.

Symlinks are now followed with `os.Stat`, so a link to a file copies the file.
Anything that is not a file after resolution is simply not copied. The original
concern survives in the part that mattered: a directory link is not descended
into.

`Copied` also carries a file count now, and setup prints a cross rather than a
tick when it is zero. The report could not previously distinguish a copy from a
no-op, which is why this went unnoticed.

**Wrong if** someone's configuration directory contains links to things they do
not intend to publish, at which point `--filter` is the answer — it names what
travels, and now defaults to naming `rules` too, which was missing entirely.

## `migrate` takes a directory and a filter

`cbx-setuptool migrate --claude-dir <dir> --filter skills,rules,agents`.

Neither was configurable before: the source was hardcoded to `~/.claude` and
the contents to a list in the source. A machine may keep more than one Claude
directory, and the one worth shipping to a box is not always the one Claude
Code reads locally.

Selection is a pure `Plan` that runs before anything touches the network, so a
filter entry naming something absent, or escaping the directory, is answered
immediately rather than after an ssh timeout against a box that was never the
problem.

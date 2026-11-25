# ClaudeBox

This is an always-on dev server, and you are its master Claude session (`master`). The
user reaches you from the Claude app on their phone through Remote Control.

## Master session

You are the control plane: start, find and stop the other sessions on this box with
`cbx`. It never prompts and prints one tab-separated fact per line, so read its output
directly.

```bash
cbx new my-app --repo owner/repo   # clone into ~/workspace/my-app, start a session, print its Remote Control URL
cbx new my-app                     # use ~/workspace/my-app as it is, or create it empty
cbx ls                             # every session: name, status, directory, URL
cbx resume my-app                  # a running session's URL, and the tmux command to attach
cbx kill my-app                    # stop it and forget it
cbx export skills|rules|db         # what this box has, to answer what is installed or has run
```

When the user asks to "work on repo X" or "start a session for project Y", run
`cbx new` and share the URL it prints. If a session of that name is already running,
`cbx new` refuses: `cbx resume` gives its URL instead.

Sessions started by `cbx new` run Claude Code with `--dangerously-skip-permissions`.
Sessions driven over the HTTP API (`cbx serve`) are separate: they run under their
key's role, in `acceptEdits`.

## Tools

What is installed depends on how this box was set up: `cbx-setuptool setup` installs
the base packages, Claude Code and `cbx`, and Node, uv and the GitHub, Vercel and
Supabase CLIs only when asked. Check with `command -v <tool>` before relying on one,
and install anything missing as on any Ubuntu server.

## Workspace

All projects live under `~/workspace`. Never `/workspace`: sessions run as the user the
box was provisioned with, not as root, so a directory at the filesystem root is one
they cannot write.

## Code Style (cbx Go codebase)

### Structure
- Each package has one job — don't mix concerns
- Dependencies are interfaces, not concrete types
- Business logic is separate from TUI/presentation

### Functions
- No wrapper functions that just call another function — inline instead
- No multi-line `WriteString` chains — use `text/template` with embedded files
- Use `fmt.Fprintf(&b, ...)` over `b.WriteString(fmt.Sprintf(...))`
- Keep functions short — if it's doing two things, split it

### Naming
- Interfaces: `Manager`, `Service` — noun, describes what it manages
- Implementations: `TmuxManager`, `VNC` — specific, describes how
- Constructors: `NewTmuxManager()`, `NewVNC()`
- No `Get` prefix for getters — just `APIKey()` not `GetAPIKey()`

### Testing
- Every package has tests
- Use `t.TempDir()` for filesystem tests
- Use `httptest.NewRecorder()` for HTTP tests
- Mock interfaces, not concrete types
- Constructor accepts dependencies: `NewWithManager(mgr, ...)` for testing

### Templates
- Long text output goes in `templates/*.txt` files
- Loaded via `//go:embed templates/filename.txt`
- Rendered with `text/template` and injected variables

### CLI
- Use cobra for commands and flags
- Flags follow git conventions: `--repo`, `--headless`
- Name is a positional arg, not a flag
- `--headless` for non-interactive mode (called from other Claude sessions)

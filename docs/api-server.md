# The API server

## Why this is not `cbx serve` coming back

[two-binaries.md](two-binaries.md) deleted `cbx serve`, its API key and its
tunnel, on the grounds that it "published an HTTP API so something could drive
sessions remotely, duplicating a capability the master session already has via
its own shell". That reasoning still holds, and this design does not reverse it.

What the old API did — create, list, kill — was duplication. What this one adds
is not:

> **send a query to a session and get the answer back in one response**

Neither SSH nor Remote Control gives you that programmatically. Remote Control
is a human at a phone; SSH gives you a shell, not a conversation. A Claude
Project, a cron job, or another agent has no way to ask this box a question and
read the reply.

So the API earns its place on the query endpoint, and the CRUD around it exists
only because a query needs something to talk to. If the query endpoint were
ever removed, the rest should go with it.

## Print mode, not the tmux pane

The obvious implementation of "send a query, get an answer" is `tmux send-keys`
followed by `capture-pane`. That is the code the decision log calls the most
fragile in the project, and it is where three separate bugs lived.

Claude Code's print mode replaces all of it:

```
claude -p --output-format json --resume <uuid> --append-system-prompt <text>
```

- `-p` prints the response and exits. **The workspace trust dialog is skipped
  in print mode** — that is bug #1 from the decision log, gone by construction
  rather than by answering a prompt we learned to recognise.
- `--output-format json` returns a single result object, which is exactly the
  "one response, not streaming tokens" contract.
- `--session-id <uuid>` lets *cbx* choose the conversation id, so nothing has
  to be parsed back out of a pane.
- `-r, --resume <uuid>` continues that conversation on the next query.

There is no screen to scrape, no "has it finished yet" heuristic, no answer
scrolling out of the capture window, and an exit code that means something.

Verified against Claude Code 2.1.236.

**Wrong if** print mode ever loses `--output-format json` or `--session-id`, at
which point the query endpoint has no honest implementation and should be
withdrawn rather than rebuilt on pane scraping.

## Sessions the API creates are headless

A cbx session is now one of two kinds. The store holds both; the API creates
only one of them.

| kind | what it is | driven by | created by |
|---|---|---|---|
| `interactive` | tmux session + Remote Control URL | a human, from a phone | `cbx new` |
| `headless` | a Claude conversation id, no tmux | `claude -p`, over the API | `POST /sessions` |

`GET /sessions` lists both, because "what is on this box" is one question.
Everything else the API offers is refused on an `interactive` session with a
`409` naming `cbx kill` or Remote Control as the way to do it. They are
**read-only through the API**.

This is not squeamishness. An interactive session and a query would be two
writers on one conversation: the phone and the API would interleave turns into
the same transcript, and neither would see a coherent history. One driver per
conversation is the rule, and the kind is what records which driver.

The cost is that you cannot ask a question of the session you are looking at on
your phone. That is a real loss, and it is the price of not rebuilding pane
scraping.

## Endpoints

Every endpoint except `/healthz` requires `Authorization: Bearer <key>`.

```
GET    /healthz                          no auth → {status, version}
POST   /auth/rotate                      → {api_key}; the old key dies immediately

POST   /sessions                         {name, repo?, system_prompt?} → headless
GET    /sessions                         both kinds, reconciled against tmux
GET    /sessions/{name}                  dir, kind, status, session_id, turns
DELETE /sessions/{name}                  forget it

POST   /sessions/{name}/query            {prompt} → 200 answer | 202 job
GET    /jobs/{id}                        poll a query that outran the threshold

PUT    /sessions/{name}/system-prompt    {prompt}
PUT    /sessions/{name}/skills/{skill}   SKILL.md body
```

### Opening an existing session needs no state

`--resume <uuid>` is stateless: the conversation lives in Claude Code's own
transcript, and cbx only stores its id. So "open a session" is a plain read of
the session row, and there is no open/close lifecycle on the server, no
connection to keep warm, and nothing to leak when a client disappears.

`DELETE` forgets the record. It does **not** delete the project directory —
`cbx kill` has always refused to delete someone's work, and an HTTP verb is not
a reason to change that. There is no process to stop either: a headless session
is a uuid between queries, not a running thing.

### Query: synchronous, with an escape hatch

A query holds the connection until Claude finishes, and returns the answer:

```
POST /sessions/api-work/query   {"prompt": "what changed in the store?"}
200  {"answer": "...", "session_id": "...", "turns": 3, "duration_ms": 8200}
```

Past a threshold (default 30s) the server stops waiting, hands back a job id,
and the caller polls:

```
202  {"job": "j_01H...", "poll": "/jobs/j_01H..."}
GET  /jobs/j_01H...   → {"status":"running"} … {"status":"done","answer":"..."}
```

This keeps the common case to one round trip while making a ten-minute query
survive a proxy that would have closed the connection. The cost is real and
worth stating plainly: **every client must handle both response shapes.** A
client that only reads `answer` will break the first time a query runs long.

**Jobs are in-memory and do not survive a restart.** That is honest rather than
lazy: a query is a child `claude -p` process, systemd restarting `cbx serve`
kills that child, and the answer no longer exists to be recovered. Persisting
the job row would only record that something was lost.

### Concurrency

One query at a time per session; a second gets `409`. Two `--resume` processes
on one conversation would append interleaved turns to the same transcript. The
lock is an in-process mutex keyed by session name, which is sufficient because
the API server is the only thing that issues queries.

### Skills and system prompts

A system prompt is stored on the session row and passed as
`--append-system-prompt` on every query. Append, not replace, so a session
keeps the box's own CLAUDE.md.

A skill is written to `<dir>/.claude/skills/<skill>/SKILL.md`, where Claude Code
finds project-scoped skills on its own. Nothing new is invented, and the skill
is confined to the session that asked for it rather than installed box-wide.

`{skill}` is matched against `^[a-z0-9][a-z0-9-]*$` before it touches a path.
This is a file write driven by a network request, and the rule from the decision
log applies with full force: quoting defends the shell and does nothing about a
`..`. The name is validated, never sanitised.

## Authentication

32 bytes from `crypto/rand`, base64url, prefixed `cbx_live_`. Stored at
`~/.local/state/cbx/api-key` with mode `0600` — the same `XDG_STATE_HOME`
convention as `sessions.db`, because it is generated data cbx can reissue.

Presented as `Authorization: Bearer <key>` and compared with
`subtle.ConstantTimeCompare`. Never `==`: a byte-at-a-time comparison leaks the
key's prefix to anyone willing to time the responses.

The key is stored in the clear. On a single-tenant box anyone who can read that
file is already root and can read `sessions.db`, every transcript and every
token on the machine — a hash would protect nothing while making the key
unrecoverable when a phone loses it. `cbx api-key show` reads it back.

**Wrong if** ClaudeBox ever runs sessions for anyone but the box's owner, which
is the same condition that would retire `IS_SANDBOX=1` and the shared root
account.

### Rotation

Three callers, one door:

```
POST /auth/rotate                 needs the current key, returns the new one
cbx api-key rotate                on the box
cbx-setuptool api rotate --host   from a laptop
```

All three call one function. The key has one definition and one writer; a
second place that issues keys is a second source of truth about what the
current key is.

Rotation takes effect immediately and the old key stops working on the next
request. There is no grace period, because the reason to rotate is usually that
the old key should already have stopped working.

## Exposure: localhost, and a deliberate act to change it

`cbx serve` binds `127.0.0.1:8091`. Nothing reaches it from outside the box by
default.

Over plain HTTP, a listener on `0.0.0.0` puts the bearer key, every prompt and
every answer on the wire in cleartext. The key is the whole of the
authentication, so that is a poor default to ship and a worse one to ship
silently.

Remote access is therefore something you ask for:

```
ssh -L 8091:localhost:8091 root@<box>      # nothing to install
cbx-setuptool api expose --host <box>      # Cloudflare tunnel, HTTPS terminated
```

## How it runs

`cbx serve` runs in the **foreground**. `cbx-setuptool` installs a systemd unit
that owns it.

No PID file, no `-d`, no self-daemonising. Commit `0147e96` — *"cbx serve -d
detaches instead of borrowing the caller's stdio"* — was a bug in exactly that
hand-rolled machinery, and systemd deletes the whole class while adding
restart-on-failure, start-on-boot and journald logs for free.

This is also the one place `cbx`'s contract bends. Its rule is that the exit
code is the result; a server does not exit. Everything else holds: `cbx serve`
reads no stdin, prompts for nothing, and prints one fact per line as it starts.

## Setup

The API is opt-in, as requested:

```
cbx-setuptool setup --host <ip> --binary ./cbx-linux --with-api
```

The step generates a key on the box, installs and enables the unit, and prints
the key and the URL once. `cbx-setuptool status` gains a line for whether the
API is running and reachable.

It is a `Step` like every other, which means it carries a `Check` that is
re-run after it — the rule earned by an installer that exited 0 having done
nothing, and the one a "reported success but is still not installed" failure
comes from.

## Schema

Three columns and a counter on `sessions`:

```sql
kind              TEXT    NOT NULL DEFAULT 'interactive'
claude_session_id TEXT    NOT NULL DEFAULT ''
system_prompt     TEXT    NOT NULL DEFAULT ''
turns             INTEGER NOT NULL DEFAULT 0
```

Existing rows default to `interactive`, which is correct: every session
recorded before this feature was a tmux session.

`turns` is what selects the flag. At `0` the conversation does not exist yet and
the first query passes `--session-id <uuid>` to create it; after that
`--resume <uuid>` continues it.

## Packages

```
internal/api/       HTTP only — routes, bearer auth, handlers, the job registry
  key.go            generate, load, verify, rotate: the key's one door
internal/claude/    the claude -p seam — build argv, exec, parse the JSON result
internal/store/     + kind, claude_session_id, system_prompt, turns
cmd/cbx/            + serve, + api-key show|rotate
internal/setuptool/ + the --with-api step, + api expose|rotate
```

`internal/claude` mirrors `internal/tmux`: a thin layer over one external
program, so the argv it builds can be unit-tested as a value and the exec is
the only part that needs a real Claude.

`internal/api` depends on interfaces, not on `*cbx.App`, so handlers can be
tested with `httptest` against a fake session source.

## Open questions

1. **Does `cbx query <name> "<prompt>"` exist?** It fits the non-interactive
   contract perfectly and would let the master session query a headless
   conversation without HTTP. Not in scope here; worth asking before the API is
   the only way in.
2. **Is `--permission-mode bypassPermissions` right for print mode?** A headless
   query has nobody to answer a prompt, same as a phone-driven session. The
   premise holds, but it is a new place to take that risk and should be a named
   constant with the reasoning attached, as `autonomousClaude` already is.
3. **What happens to a headless session's transcript on `DELETE`?** The row goes.
   Claude Code's own `.jsonl` stays on disk. That is probably right — it is the
   record of work — but nothing prunes it.
4. **Job TTL.** Results sit in memory until read. A client that never polls
   leaks one answer's worth of memory per query.

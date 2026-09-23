# The API server

<p align="center">
  <img src="architecture.svg" width="960" alt="cbx-setuptool provisions the box from a laptop. On the box, the cbx CLI creates interactive tmux sessions reached from a phone by Remote Control, while cbx serve exposes an HTTP API driving headless claude -p sessions. Both record state in one SQLite database." />
</p>

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
GET    /openapi.yaml · /openapi.json     the API, describing itself
POST   /auth/rotate                      → {api_key}; the old key dies immediately

GET    /commands                         the slash-command allowlist
PUT    /commands                         replace it (YAML or JSON)

POST   /sessions                         {name, repo?, system_prompt?,
                                          permission_mode?, model?, effort?,
                                          skills?, respond_within?}
GET    /sessions                         both kinds, reconciled against tmux
GET    /sessions/{name}                  dir, kind, status, session_id, turns
DELETE /sessions/{name}                  forget it

POST   /sessions/{name}/query            {prompt, respond_within} → 200 | 202 job
GET    /jobs/{id}?respond_within=        poll, or long-poll, a query in flight
DELETE /jobs/{id}                        cancel a running query, or discard a result

POST   /sessions/{name}/command          {command, respond_within?} → declared
                                          slash commands only

PUT    /sessions/{name}/system-prompt    {prompt}
PUT    /sessions/{name}/skills/{skill}   SKILL.md body

GET    /sessions/{name}/artifacts        what it may hand back
GET    /sessions/{name}/artifacts/{path} fetch one
```

### Opening an existing session needs no state

`--resume <uuid>` is stateless: the conversation lives in Claude Code's own
transcript, and cbx only stores its id. So "open a session" is a plain read of
the session row, and there is no open/close lifecycle on the server, no
connection to keep warm, and nothing to leak when a client disappears.

`DELETE` forgets the record and deletes the conversation transcript at
`~/.claude/projects/<escaped-cwd>/<uuid>.jsonl`. Closing a session should not
leave its history readable on the box afterwards.

It does **not** delete the project directory. `cbx kill` has always refused to
delete someone's work, and an HTTP verb is not a reason to change that — the
transcript is the session, the directory is the work.

Between queries there is no process to stop: a headless session is a uuid, not a
running thing. During one there is, and `DELETE` cancels it before forgetting
the session rather than refusing. That follows `cbx kill`, which succeeds on a
session that is already gone because *the intent is that it be gone* — the same
reading applies when the obstacle is a query still in flight.

### Skills can be run into a session as it is created

```
POST /sessions
{ "name": "review-bot", "skills": ["inline", "testing"], "respond_within": "3m" }
```

Each skill is invoked the way Claude Code invokes one — as `/name`, a turn of
its own. That is not free: a skill measured at twenty seconds of API time. So
priming follows the query contract rather than inventing a second one, and
`respond_within` is required whenever `skills` is present. The session is
created either way; `priming` in the response says whether the skills got in,
and carries a job to poll when they did not finish in time.

**An unknown skill reports success.** `/not-a-skill` answers
`Unknown command: /not-a-skill` with `is_error: false` and `turns: 0` — the
same shape as a command handled locally. Priming therefore checks the answer
rather than the exit code, because the alternative is a session created
claiming to know something nothing ever taught it.

It stops at the first skill that fails. A session primed with half of what was
asked for is worse than one that says which half failed: the caller's
assumption about what the session knows is already wrong, and continuing would
bury that under later output.

**Two consequences of creation running turns**, both deliberate and both
pinned by tests, because a caller meets them without having done anything
themselves:

- **A query sent while priming is still running gets `409`.** Priming holds the
  session's one running-query slot, which is the same rule that stops two
  `--resume` processes interleaving. The session exists before it is usable,
  and `priming.poll` is how to know when that changes.
- **A `201` can carry `priming.status: failed`.** Creation succeeded; priming
  did not. The session is real, usable, and unprimed — rolling it back would
  discard something that works because something optional did not.

The alternative was deferring the skills to the first query, which would have
removed both. It was rejected: knowing at creation whether a session got its
instructions is worth more than an instant `201`, because the answer arrives
while the caller is still in a position to do something about it.

### Query: the caller says how long it will wait

A query holds the connection until Claude finishes, and returns the answer:

```
POST /sessions/api-work/query
{
  "prompt": "what changed in the store?",
  "respond_within": "90s"
}

200  {"answer": "...", "session_id": "...", "turns": 3, "duration_ms": 8200}
```

`respond_within` is **required, and has no default.** The server knows how long
Claude usually takes; only the caller knows what *it* can hold open. A phone on
a train, a Cloudflare tunnel and a cron job have three different answers, and
any constant the server picked would be wrong for at least two of them — so it
does not pick one. Omitting the field is `400`, not a guess.

A bare integer is seconds; a Go duration string (`90s`, `5m`) works too, so
`"5m"` does what it obviously means instead of failing to parse. It is in the
body rather than the query string because it is part of what is being asked,
alongside the prompt.

When the window closes, the caller gets a job id and polls:

```
202  {"job": "j_01H...", "poll": "/jobs/j_01H..."}
GET  /jobs/j_01H...   → {"status":"running"} … {"status":"done","answer":"..."}
```

**The name is the contract: the server responds within that window.** Both
outcomes honour it — the answer if it is ready, a job id if it is not. What the
field does *not* do is stop the work, which is why it is not called `timeout`.
Claude keeps going, the answer lands in the job, and it is waiting whenever the
caller asks. Nothing is lost by naming a short window; it changes only whether
you are told now or told later.

`"0s"` is therefore useful rather than degenerate: it returns `202` at once
without waiting, which is exactly what a fire-and-forget caller wants.

The ceiling is fifteen minutes. Above it the request is refused with `400`
naming the maximum, rather than silently clamped — a server that quietly does
something other than what was asked is the failure this project keeps finding in
other people's installers, and it should not ship one.

The same field long-polls a job, so a caller that wants the answer the moment it
exists does not have to spin. Here it is a query parameter and it is optional,
because `GET` has no body and "tell me the status now" is the obvious default
for a read:

```
GET /jobs/j_01H...?respond_within=60s    → waits up to 60s for it to finish
GET /jobs/j_01H...                       → answers immediately
```

**Implementation note, because this is where it will break:** the server's own
`WriteTimeout` must exceed the maximum `respond_within` it accepts, or the
transport closes connections the field explicitly permitted. `ReadHeaderTimeout`
stays short; it defends against a slow-header client and is unrelated to how
long a handler may run.

The cost is real and worth stating plainly: **every client must handle both
response shapes.** One that only reads `answer` will break the first time a
query outruns its own window.

### Commands are declared in a spec, not guessed at

One endpoint sends a slash command to a session:

```
POST /sessions/api-work/command   {"command": "/model sonnet"}
```

What it will do is not inferred from the command string. A YAML spec on the box
declares which commands the API accepts and how each one is carried out, and the
server reads it on every request, so adding a command is an edit rather than a
release.

```yaml
# ~/.config/cbx/commands.yaml
version: 1
commands:
  - name: /clear
    effect: rotate-session      # cbx implements it; forwarding silently fails
    description: Start a fresh conversation, discarding history
  - name: /model
    effect: forward
    requires_args: true         # bare /model only prints its usage
  - name: /compact
    effect: forward
    expect_empty: true
```

Anything not named in `commands` is refused with `400`. **Deny by default is
not caution here, it is the only option available** — see below.

`effect` is what the server does, and the two values exist because forwarding is
not always capable of the thing the command names:

| effect | server does |
|---|---|
| `forward` | runs it through `claude -p --resume <uuid>` and returns the result |
| `rotate-session` | cbx performs it natively; `/clear` is the case that needs this |

`respond_within` is required when `effect` is `forward`, for the same reason a
query needs it: the command runs through `claude -p` and may take a while. A
`rotate-session` command never asks, because cbx performs it without running
anything.

It lives under `XDG_CONFIG_HOME`, not `XDG_STATE_HOME`. This is hand-edited
policy, not generated data cbx can rebuild — the opposite of `sessions.db`. The
shipped default is `commands.example.yaml` at the project root, embedded in the
binary and uploaded by `cbx-setuptool` — never over an edited file, because the
operator's policy outranks ours.

The spec is also readable and replaceable over HTTP, so a box's policy can be
managed without an ssh session. `PUT /commands` validates before writing — a
spec that does not parse would take the command endpoint down with it, and the
file already on disk is a working one. Comments do not survive a replacement,
which is a reason to keep editing the file on the box when the reasoning
matters.

This does not widen what a caller can do. Anyone holding the key can already
create a session and send it any prompt; the allowlist governs slash commands,
which are the narrower power. It guards against a mistake, not an attacker.

### The API describes itself

`openapi.yaml` at the project root, embedded and served at `/openapi.yaml` and
`/openapi.json`. Behind the key like everything else: once a box is exposed
through a tunnel, an unauthenticated description would tell the internet
exactly what is listening, and anyone entitled to call the API has a key
already.

Routes are declared in a table that both registers them and is checked against
the document — in both directions, so a described endpoint that does not exist
and an endpoint nobody described are each a failing test. A published
description that has drifted from the server is worse than none, because it is
believed.

### Why the spec has to be an allowlist

Measured against Claude Code 2.1.236, in print mode:

| command | `is_error` | `turns` | `result` |
|---|---|---|---|
| `/model` | false | 0 | usage text, listing the available models |
| `/model sonnet` | false | 0 | `Set model to Sonnet 5 for this session only` |
| `/compact` | false | 0 | *empty* |
| `/clear` | false | 0 | *empty* — and the conversation is **not** cleared |
| `/help` | false | 0 | `/help isn't available in this environment.` |
| `/nonexistent-command-xyz` | false | 0 | `Unknown command: /nonexistent-command-xyz` |

Every row reports success. A command that does not exist reports success. A
command refusing to run in this environment reports success. **There is no
programmatic signal to gate on**, which is why the gate has to be a list
somebody wrote down, and why detection cannot be promoted into one.

`turns: 0` is also not a failure signal — it means the command was handled
locally without a model call, which is true of every one of these including the
ones that worked.

Two things the spec still cannot catch, so the server checks them anyway:

- **A forked conversation.** `claude -p` returns the `session_id` it actually
  used. When that differs from the one requested, the command ran somewhere
  other than the session it was aimed at, and the response says so rather than
  reporting a success that landed elsewhere.
- **`/clear` in particular.** Forwarded, it returns empty, reports success,
  forks a new id, and leaves the transcript intact — verified by asking for a
  codeword afterwards and getting it back. That is why it is `rotate-session`
  and not `forward`: cbx allocates a new conversation id and resets `turns`, and
  the history is genuinely gone from the session's point of view.

### Getting work back out

A session writes its output into its own directory, and a caller needs to
fetch it. What a caller must not get is everything else in there — a cloned
repository, scratch files, a stray `.env`, whatever a turn happened to write.

So the fetchable set is **declared, not discovered**. A query names the files
it expects to produce; when it finishes, those that exist are registered and
become fetchable. Nothing else ever is.

```
POST /sessions/x/query   { prompt, respond_within, artifacts: ["report.html"] }
GET  /sessions/x/artifacts                → the register
GET  /sessions/x/artifacts/report.html    → the bytes
```

This is the same deny-by-default the command allowlist uses, for the same
reason: the alternative is a boundary that holds only while everyone behaves.
Confining reads to the session directory is not enough when the directory is a
cloned repository.

Declared paths are validated **before** the turn runs, so a typo costs a round
trip rather than a report. A declared file the turn never wrote is simply not
registered — absent from the listing rather than a fetch that fails.

Artifacts expire after four hours — **the registration and the file it names.**
Expiring only the register would leave every report a session ever produced on
disk for ever, merely unfetchable, and these files hold whatever the session
was asked to write about.

That does not contradict `DELETE` keeping the working directory. `DELETE`
removes a *session*, and the directory is the work; an artifact is a declared
output with a stated lifetime, and deleting it at expiry is what the lifetime
promised. The janitor only ever removes files that were registered as outputs,
and resolves each inside its session again before unlinking — everything else
in the directory was never an artifact.

Never declared, expired, and no longer on disk all answer `404` alike. A
caller learns what it may fetch from the register, not by probing the
filesystem.

Containment is checked twice — once when a path is declared, and again on
every fetch, because a symlink planted after registration would otherwise turn
a registered path into a way out.

### Jobs are rows, and a janitor sweeps them

A job is a row with an `expires_at` set when it is created. A janitor goroutine
lives with the server and sweeps every ten minutes.

Expiry is **data, not a runtime timer**. A timer only exists inside the process
that scheduled it, so a crash loses every pending deletion and leaves rows no
one will ever collect. A date on a row survives the process that wrote it: the
janitor converges the table from whatever state a crash left behind, without
depending on traffic arriving to trigger a lazy sweep.

`DELETE /jobs/{id}` is the manual path, and means the same thing in both states
a job can be in — *I am done with this*:

| job state | what `DELETE` does |
|---|---|
| running | `SIGTERM` the child, `SIGKILL` after a grace period, mark it cancelled |
| finished | discard the result now rather than at `expires_at` |

**The sweep only deletes finished rows.** A query legitimately still running
past its own `expires_at` must keep its record; deleting it would pull the row
out from under a client mid-flight.

### Two things persistence forces

**A restart must reconcile its own wreckage.** Job rows outlive the `claude -p`
children that produce them. Without a startup sweep a client polling after a
restart sees `status: running` for ever — a ghost, and a lie, since nothing is
working on it:

```sql
UPDATE jobs SET status = 'interrupted' WHERE status = 'running';
```

This is strictly better than keeping jobs in memory, where a restart gave the
client a bare `404` that could not distinguish "never existed" from "died in a
restart". Here the client is told what happened.

**Children must die with the server**, or the orphan keeps writing to a
transcript nothing is tracking. Stated in the unit rather than assumed:

```ini
[Service]
KillMode=control-group
Restart=on-failure
```

### Concurrency

One query at a time per session; a second gets `409`. Two `--resume` processes
on one conversation would append interleaved turns to the same transcript.

Because jobs are rows, the database enforces this rather than a mutex:

```sql
CREATE UNIQUE INDEX one_running_per_session
  ON jobs(session_name) WHERE status = 'running';
```

An in-memory lock would be forgotten on restart — and combined with an orphaned
child, that is exactly how a second `--resume` gets started on a conversation
that is already being written to. The startup `UPDATE` above also clears this
index, so no session stays wedged by a job that died with the last process.

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

## How it runs: a background service, supervised

The API is a background service. It starts at boot, survives the logout of
whoever started it, restarts if it dies, and is never attached to a terminal.

`cbx serve` runs in the foreground by default and backgrounds itself with
`--detach`. Which one is right depends on what, if anything, is supervising it:

```
droplet          systemd unit, Type=simple   ← cbx-setuptool installs it
Railway/Docker   CMD ["cbx", "serve"]        ← PID 1 is the service
ad hoc, no init  cbx serve --detach          ← nothing else is watching
```

`Type=simple` means "this process does not background itself", which is the
supervisor's job and not the same thing as running in a terminal. Where a
supervisor exists it should be the one doing this, because it also gives
restart-on-failure, start-on-boot and log capture, none of which `--detach` can.

**`--detach` must never be a container's `CMD`.** That is a constraint, not a
preference: a PID 1 that forks and exits takes the container down with it, so
the Railway row stays foreground permanently.

### What `--detach` has to get right

Commit `0147e96` — *"cbx serve -d detaches instead of borrowing the caller's
stdio"* — is the prior art, and it is worth naming what it got wrong so this
does not repeat it.

**Re-exec, not fork.** Go cannot `fork()` safely; the runtime is multi-threaded
and only the calling thread survives. The child is started with
`exec.Command(os.Executable(), ...)` and `SysProcAttr{Setsid: true}`.

**`Setsid` is the part that matters.** Without a new session the child keeps the
caller's controlling terminal and dies of `SIGHUP` when the SSH connection
closes — detached in appearance only, which is the worst version of this
feature.

**Stdio goes nowhere near the caller.** That was the literal 0147e96 bug: stdin
from `/dev/null`, stdout and stderr to `~/.local/state/cbx/serve.log`. A daemon
holding a terminal open keeps the SSH session from closing.

**A lock file, not a PID file.** `~/.local/state/cbx/serve.lock` is held with
`flock` for as long as the process lives, and the kernel releases it when the
process dies however it died. "Is it running?" becomes "can I take the lock?",
which a bare PID file cannot answer — a PID outlives its process and gets
recycled, so a stale file reports a running server, or worse, `--stop` signals
whatever unrelated process inherited the number. The PID is written inside the
locked file for reporting; the lock is what is trusted.

**`--stop` signals the process group, not the process.** Under systemd,
`KillMode=control-group` kills query children with the service. Detached, there
is no cgroup and that has to be done by hand: children are started with
`Setpgid`, and `--stop` sends `SIGTERM` to the group, then `SIGKILL` after a
grace period. Skipping this leaves orphaned `claude -p` processes writing to
transcripts nothing is tracking — the failure already described under
[Two things persistence forces](#two-things-persistence-forces).

**Nothing restarts it.** This is the cost of the flag rather than a defect in
it. A detached server that dies stays dead, and its `running` job rows stay
`running` until someone starts it again and the boot sweep marks them
`interrupted`. Under systemd that window is the length of a restart; detached
it is the length of time before a human notices.

The unit sets `KillMode=control-group` so query children die with the service;
the reasoning is under [Two things persistence forces](#two-things-persistence-forces).

This is the one place `cbx`'s contract bends. Its rule is that the exit code is
the result; a server does not exit. Everything else holds: `cbx serve` reads no
stdin, prompts for nothing, and prints one fact per line as it starts.

## Setup

The API is opt-in:

```
cbx-setuptool setup --host <ip> --binary ./cbx-linux --with-api
```

The step uploads the command spec, writes the systemd unit, enables and starts
it, and prints the key. `cbx-setuptool status` gains a line for whether the
service is running, and the API can be managed afterwards on its own:

```
cbx-setuptool api install --host <ip>    unit, spec and key in one go
cbx-setuptool api key     --host <ip>    what the box currently accepts
cbx-setuptool api rotate  --host <ip>    new key, and restart onto it
cbx-setuptool api forward --host <ip>    the ssh tunnel that reaches it
```

Rotation restarts the service deliberately: a running server holds the key it
started with, so rewriting the file alone would leave the old key working until
something happened to restart it.

Two ways to reach it, because they serve different callers:

```
cbx-setuptool api forward --host <ip>   ssh -N -L, from a machine that can ssh
cbx-setuptool api expose  --host <ip>   a public HTTPS tunnel, for everything else
cbx-setuptool api url     --host <ip>   the tunnel's current hostname
```

`forward` needs nothing installed and encrypts the hop, but only works from a
machine with ssh access to the box — which a phone and a Claude Project are
not.

`expose` installs `cloudflared` and a second unit running a **quick tunnel**:
HTTPS terminated at Cloudflare, no account required, and a hostname that
changes every time the tunnel restarts. `url` reads the current one, because
anything holding the old one is holding something that no longer resolves.

`cloudflared` is a `Step` — so it inherits the re-check that caught an
installer exiting 0 having installed nothing — but deliberately **not** one of
`InstallSteps`. A box only needs it if somebody decides to expose that box, and
that decision should be made rather than inherited.

Once exposed, the bearer key is the only thing between the internet and these
sessions. `api expose` says so on stderr rather than leaving it to be
realised.

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
permission_mode   TEXT    NOT NULL DEFAULT ''
model             TEXT    NOT NULL DEFAULT ''
effort            TEXT    NOT NULL DEFAULT ''
turns             INTEGER NOT NULL DEFAULT 0
```

`permission_mode` is set once, when the session is created, and passed to every
query as `--permission-mode`. It is a property of the session rather than of a
request: a caller that could raise its own permissions per query would make the
setting meaningless. Empty means cbx's default for a headless session, which is
the same `bypassPermissions` premise the tmux sessions run on and named in the
same deliberate way — a query has nobody to answer a prompt either.

Accepted values are Claude Code's own: `acceptEdits`, `auto`,
`bypassPermissions`, `manual`. Anything else is `400` at session creation
rather than a flag rejected later by a child process nobody is watching.

`model` and `effort` are fixed at creation for the same reason, because Claude
Code describes both as belonging to a session. `effort` is a closed set —
`low`, `medium`, `high`, `xhigh`, `max` — and validated against it. `model` is
not: aliases, full names and context variants (`opus`, `claude-fable-5`,
`opus[1m]`) are an open set that a hardcoded list would fall behind, so its
*shape* is validated instead.

The shape check is not cosmetic. `--model` takes its value before the `--`
that ends option parsing, so a name beginning with a dash reaches Claude as
another flag. That is the same distinction that produced a command injection
through `git clone` earlier in this project, and it is now guarded in a third
place.

Existing rows default to `interactive`, which is correct: every session
recorded before this feature was a tmux session.

`turns` is what selects the flag. At `0` the conversation does not exist yet and
the first query passes `--session-id <uuid>` to create it; after that
`--resume <uuid>` continues it.

And a `jobs` table, in the same database for the same reason the sessions table
is there — two writers, and a row that must outlive the process that made it:

```sql
CREATE TABLE IF NOT EXISTS jobs (
  id           TEXT PRIMARY KEY,
  session_name TEXT    NOT NULL,
  status       TEXT    NOT NULL,      -- running | done | failed | cancelled | interrupted
  prompt       TEXT    NOT NULL,
  answer       TEXT    NOT NULL DEFAULT '',
  error        TEXT    NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS one_running_per_session
  ON jobs(session_name) WHERE status = 'running';
```

`interrupted` is its own status rather than `failed`: it says the server
restarted underneath a query, which is a different thing from Claude failing and
suggests a different response from the client.

## Packages

```
internal/api/       HTTP only — routes, bearer auth, handlers
  key.go            generate, load, verify, rotate: the key's one door
  janitor.go        the sweep loop, started with the server, stopped with it
internal/claude/    the claude -p seam — build argv, exec, parse the JSON result
internal/store/     + kind, claude_session_id, system_prompt, turns; + jobs
  detach.go         re-exec with Setsid, the flock, --stop's group signal
  commands.go       load the spec, resolve a command to its effect
cmd/cbx/            + serve [--detach|--stop], + api-key show|rotate
internal/setuptool/ + the --with-api step, + api expose|rotate
```

`internal/claude` mirrors `internal/tmux`: a thin layer over one external
program, so the argv it builds can be unit-tested as a value and the exec is
the only part that needs a real Claude.

`internal/api` depends on interfaces, not on `*cbx.App`, so handlers can be
tested with `httptest` against a fake session source.

## Decided, with what would make each wrong

**No `cbx query` on the CLI.** The API is the way in. It would have been a
second door onto the same conversation for no capability the first does not
already have. *Wrong if* the master session ever needs to query a headless
conversation while the API server is down, which is an argument for making the
server more reliable rather than for a second entry point.

**`permission_mode` is per session, set at creation, in the request body.** It
is a property of the session, not of a request: a caller that could raise its
own permissions per query would make the setting meaningless. *Wrong if* a
single session genuinely needs different permissions per task, at which point
the answer is two sessions.

**`DELETE` removes the transcript.** Closing a session should not leave its
history readable on the box. The project directory survives. *Wrong if* someone
expects `DELETE` to be recoverable — it is not, and the endpoint should say so
in its error text rather than in this file.

**A finished job is kept one hour.** Long enough that a dropped response can be
retried, short enough that unread answers do not accumulate. *Wrong if* a phone
that polls the next morning turns out to be the normal case, which would make
this a day.

**YAML for the command spec**, `gopkg.in/yaml.v3`, accepted as a fourth
dependency. The file exists to be hand-edited and JSON is unpleasant to
hand-edit. *Wrong if* the spec stops being hand-edited and becomes generated, at
which point the dependency buys nothing the standard library does not.

## Settled by experiment

Recorded because each of these was going to be guessed at, and two of the
guesses would have been wrong. All against Claude Code 2.1.236.

**`--resume` against an unknown id fails loudly.** It prints
`No conversation found with session ID: <uuid>` as plain text, not JSON, so it
cannot be mistaken for a result. The crash window between `turns` and the
conversation actually existing therefore has a safe resolution: attempt
`--resume`, and fall back to `--session-id` on that error. Ordering the `turns`
write no longer has to be perfect.

**`--session-id` creates a conversation and `--resume` continues it**, with the
id stable across resumes. Seeded a codeword, resumed, got it back.

**Slash commands execute locally in print mode** — `turns: 0`,
`duration_api_ms: 0`, no tokens spent. They are not sent to the model as
prompts.

**`/clear` does not clear a resumed conversation.** It reports success, forks a
fresh id, and leaves the transcript intact; the codeword was still recalled
afterwards. This is the reason `/clear` is `rotate-session` rather than
`forward`.

**Nothing hangs.** Commands that open a picker interactively return in a few
seconds in print mode rather than waiting on input that cannot arrive. There is
no stuck child holding a session lock to design around.

**A killed turn leaves a resumable conversation.** `SIGKILL` to a `claude -p`
ten seconds into a long answer still left a 31-line transcript, and `--resume`
continued from it with the session id intact. Claude even described the state
correctly — *"You asked for a 1200-word essay on the history of the bicycle,
which I haven't written yet."*

This is what makes `DELETE /jobs/{id}` safe to offer. Cancelling costs the turn
in flight and nothing else; there is no corrupted transcript to repair and no
session to quarantine afterwards.

**Transcripts live at `~/.claude/projects/<escaped-cwd>/<uuid>.jsonl`**, which
is how `DELETE /sessions/{name}` finds the one to remove. The directory name is
the working directory with separators escaped, so it is derivable from what the
session row already stores.

## Test spec

Every test is **ADD** — none of this exists yet. Go's testing package
throughout; `unit` needs nothing external, `integration` drives a real SQLite
file and a real `claude` binary and skips when one is absent, the way
`tests/session_lifecycle_test.go` already skips without tmux.

Bodies are omitted deliberately. Names state the behaviour; the bodies land
with the implementation.

### `internal/api` — auth (unit, httptest)

| test | asserts |
|---|---|
| `TestHealthzNeedsNoKey` | `/healthz` answers `200` with no header at all |
| `TestEveryOtherRouteRejectsAMissingKey` | parameter rows over each route → `401` |
| `TestAWrongKeyIsRejected` | a well-formed key that is not the stored one → `401` |
| `TestKeyComparisonIsConstantTime` | asserts `subtle.ConstantTimeCompare` is reached, not timing — the guard is that `==` never appears on the key path |
| `TestRotateRequiresTheCurrentKey` | rotate without a valid key → `401`, and the key is unchanged |
| `TestTheOldKeyDiesImmediatelyAfterRotation` | old key → `401` on the very next request |
| `TestRotateReturnsAKeyThatWorks` | the returned key authenticates |

### `internal/api` — sessions (unit, httptest, fake session source)

| test | asserts |
|---|---|
| `TestCreateMakesAHeadlessSession` | `kind` is `headless`; a conversation id is allocated; `turns` is `0` |
| `TestCreateStoresThePermissionMode` | the mode from the body reaches the row |
| `TestCreateRejectsAnUnknownPermissionMode` | rows over `acceptEdits, auto, bypassPermissions, manual, nonsense` → last one `400` |
| `TestListReportsBothKinds` | a tmux session and a headless one both appear |
| `TestMutatingAnInteractiveSessionIsRefused` | rows over query/command/delete → `409` naming Remote Control |
| `TestDeleteRemovesTheRowAndTheTranscript` | the `.jsonl` is gone afterwards |
| `TestDeleteLeavesTheProjectDirectory` | the working directory survives |
| `TestDeleteCancelsAQueryInFlight` | a running job for that session ends `cancelled` |

### `internal/api` — query and jobs (unit, httptest)

| test | asserts |
|---|---|
| `TestQueryRequiresRespondWithin` | omitted → `400`; the message names the field |
| `TestRespondWithinAcceptsSecondsAndDurations` | rows: `90`, `"90s"`, `"5m"` all parse to the same window |
| `TestRespondWithinAboveTheCeilingIsRefused` | `"20m"` → `400` naming the maximum, and is **not** clamped |
| `TestZeroRespondWithinReturnsAJobImmediately` | `"0s"` → `202` without waiting |
| `TestAnAnswerInsideTheWindowReturns200` | body carries `answer`, `session_id`, `turns` |
| `TestAnAnswerOutsideTheWindowReturns202` | body carries `job` and `poll`; the query keeps running |
| `TestASecondQueryOnABusySessionIsRefused` | `409`; the unique index is what rejects it |
| `TestJobPollReturnsRunningThenDone` | status transitions without long-polling |
| `TestJobLongPollWaitsForCompletion` | `?respond_within=` on `GET /jobs/{id}` |
| `TestDeletingARunningJobCancelsIt` | child is signalled; status becomes `cancelled` |
| `TestDeletingAFinishedJobDiscardsIt` | subsequent `GET` → `404` |

### `internal/api` — the command spec (unit)

| test | asserts |
|---|---|
| `TestACommandNotInTheSpecIsRefused` | deny by default → `400` |
| `TestRequiresArgsIsEnforced` | bare `/model` → `400`; `/model sonnet` proceeds |
| `TestClearRotatesTheConversationInsteadOfForwarding` | a new conversation id, `turns` back to `0`, and no `claude -p` invocation |
| `TestForwardEffectRunsTheCommand` | argv carries the command text |
| `TestAForkedSessionIdIsReportedNotSwallowed` | returned id ≠ requested → `409`, never a `200` |
| `TestSpecIsRereadPerRequest` | editing the file changes behaviour with no restart |
| `TestAMalformedSpecIsAnErrorNotAnEmptyAllowlist` | refuses to start rather than silently denying everything |

### `internal/claude` — argv construction (unit, pure)

The whole point of the seam: argv is a value, so it is asserted without running
Claude.

| test | asserts |
|---|---|
| `TestFirstQueryCreatesTheConversation` | `turns == 0` → `--session-id <uuid>`, never `--resume` |
| `TestLaterQueriesResumeIt` | `turns > 0` → `--resume <uuid>` |
| `TestSystemPromptIsAppendedNotReplaced` | `--append-system-prompt`, never `--system-prompt` |
| `TestPermissionModeIsPassedThrough` | `--permission-mode <mode>` when set, absent when empty |
| `TestOutputIsAlwaysJSON` | `--output-format json` on every invocation |
| `TestResultParsesAnswerAndSessionId` | rows over real captured JSON payloads |
| `TestMissingConversationIsRecognised` | `No conversation found with session ID` → a typed error, not a parse failure |
| `TestAMissingConversationFallsBackToCreating` | that error retries with `--session-id` |

### `internal/store` — jobs (unit, real SQLite)

| test | asserts |
|---|---|
| `TestJobRoundTrips` | insert, read back, fields intact |
| `TestOnlyOneRunningJobPerSession` | a second insert violates the unique index |
| `TestAFinishedJobFreesTheSession` | the index permits the next one |
| `TestSweepDeletesExpiredFinishedJobs` | injected clock; no sleeping |
| `TestSweepSparesARunningJobPastItsExpiry` | the row a client is still waiting on survives |
| `TestBootRecoveryMarksRunningJobsInterrupted` | and thereby clears the unique index |
| `TestExistingSessionsMigrateToInteractive` | a pre-feature database gains the columns with `kind = 'interactive'` |

### `internal/api` — detach (unit + integration)

| test | asserts |
|---|---|
| `TestDetachedServerSurvivesItsParent` | integration; the parent exits, the server answers `/healthz` |
| `TestTheLockIsHeldWhileRunning` | a second `serve` refuses to start |
| `TestTheLockIsReleasedWhenTheProcessDies` | `SIGKILL` the server, the lock is takeable — this is what a PID file cannot do |
| `TestStopSignalsTheProcessGroup` | a child started with `Setpgid` receives it |
| `TestDetachRedirectsStdio` | the caller's stdout is untouched — the `0147e96` regression |

### `internal/api` — skills (unit)

| test | asserts |
|---|---|
| `TestSkillIsWrittenUnderTheSessionDirectory` | `<dir>/.claude/skills/<name>/SKILL.md` |
| `TestSkillNameIsValidatedNotSanitised` | rows: `..`, `../x`, `a/b`, `A`, `-x` → all `400`; `my-skill` accepted |

### `tests/` — end to end (integration)

| test | asserts |
|---|---|
| `TestCreateQueryAnswerLifecycle` | a real `claude -p` answers a real HTTP request |
| `TestClearActuallyForgetsTheCodeword` | seed a codeword, `/clear`, ask again — the measured trap, as a regression test |
| `TestCancelledQueryLeavesAResumableSession` | kill mid-turn, resume, the session still works |

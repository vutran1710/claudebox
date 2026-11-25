<p align="center">
  <img src="logo.svg" width="200" />
</p>

# ClaudeBox

Claude Code on a machine you own: always on, open from the Claude app on your phone,
and callable over HTTP as the engine of your own AI service.

## The problems

1. **An AI service needs paid API access.** You already have Claude, the strongest AI
   tool available, yet prototyping a service on it starts with buying API credits and
   building an agent.
2. **Claude Code is tied to your development machine.** It is at its most capable
   there, and that machine does not go where you go.
3. **Claude app sessions are scattered.** Each session in the app is rich on its own,
   but they stay separate, with nothing that coordinates them as one piece of work.
4. **Claude in the cloud lacks the tooling to ship.** Deploying end to end takes
   Docker, wrangler, the Vercel CLI and your own logins, which a hosted sandbox does
   not carry.
5. **Your secrets cannot move into the Claude app.** The tokens and keys your tools
   depend on have nowhere to live there.
6. **Your laptop has to stay awake.** A session you follow from your phone stops when
   the laptop running it sleeps.

## ClaudeBox

A Linux machine you own runs Claude Code around the clock, managed by `cbx` on the
box and set up by `cbx-setuptool` from your laptop.

<p align="center">
  <img src="docs/architecture.svg" width="960" alt="cbx-setuptool provisions the box from a laptop. On the box, the cbx CLI creates interactive tmux sessions reached from a phone by Remote Control, while cbx serve exposes an HTTP API driving headless claude -p sessions. Both record state in one SQLite database." />
</p>

1. **Use the Claude you have as your AI service.** `cbx serve` puts your signed-in
   Claude Code behind an HTTP API: give a session its rules, model and knowledge, send
   it a question, and collect the answer and any files it wrote. There is no agent to
   build.
2. **Claude Code wherever you are.** It runs on the box, not your laptop, and every
   session opens in the official Claude app through Remote Control.
3. **One session coordinates the rest.** A master session starts, lists and stops the
   others on the box. They share one environment, and each remains a session of its
   own in the Claude app.
4. **A complete environment for shipping.** Node, uv and the GitHub, Vercel and
   Supabase CLIs install on request; Docker, wrangler or anything else installs as on
   any Linux server. Every session can use all of it, and it persists between runs.
5. **Your secrets stay on your box.** Tool logins are made there from tokens piped over
   SSH. Values your service passes to a session reach its tools as environment
   variables, never the prompt, and are only ever listed by name.
6. **Close the laptop.** The box is always on, so your sessions keep running and stay
   reachable from your phone.

## Quick start

```bash
curl -fsSL -o cbx-setuptool \
  https://github.com/vutran1710/claudebox/releases/latest/download/cbx-setuptool-darwin-arm64
chmod +x cbx-setuptool
./cbx-setuptool setup --host <ip> --with-api   # any Ubuntu machine you can ssh into
ssh <ip> cbx new master                         # open the URL it prints on your phone
```

## Read next

- [docs/guide.md](docs/guide.md): set up a box, run sessions, drive the HTTP API
- [openapi.yaml](openapi.yaml): the API contract, also served by the box
- [docs/api-server.md](docs/api-server.md): how the API server works, and why
- [docs/two-binaries.md](docs/two-binaries.md): why a laptop tool and a box tool
- [docs/decision-log.md](docs/decision-log.md): choices made, and what would make each wrong

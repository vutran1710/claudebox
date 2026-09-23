#!/usr/bin/env bash
# Smoke test: drive the HTTP API against the Claude Code on this machine.
#
# scripts/smoke.sh provisions a droplet and exercises the CLI. This is its
# counterpart for the API, and it runs locally because the thing it has to
# prove needs a *signed-in* Claude — which a droplet in CI does not have, and
# a laptop usually does.
#
# It uses its own XDG directories and its own port, so it never touches a real
# box's database, key or sessions.
#
#   scripts/smoke-api.sh
#
set -euo pipefail

PORT="${PORT:-8199}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
export XDG_STATE_HOME="$WORK/state"
export XDG_CONFIG_HOME="$WORK/config"
BASE="http://127.0.0.1:$PORT"
CBX="$WORK/cbx"

cleanup() {
  "$CBX" serve --stop >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
ok()   { echo "  ok   $*"; }

api() {
  local method="$1" path="$2"; shift 2
  curl -sS -X "$method" -H "Authorization: Bearer $KEY" \
    -H "Content-Type: application/json" "$BASE$path" "$@"
}

code() {
  local method="$1" path="$2"; shift 2
  curl -sS -o /dev/null -w '%{http_code}' -X "$method" \
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
    "$BASE$path" "$@"
}

echo "--- building"
( cd "$ROOT" && go build -o "$CBX" ./cmd/cbx )

if ! command -v claude >/dev/null; then
  fail "claude is not installed — this test exists to exercise a real one"
fi
if ! claude auth status --json 2>/dev/null | grep -q '"loggedIn": *true'; then
  fail "claude is not signed in — sign in, or run the unit tests instead"
fi
ok "claude is installed and signed in"

echo "--- starting the API"
"$CBX" serve --detach --addr "127.0.0.1:$PORT" >/dev/null
for _ in $(seq 1 25); do
  curl -sf "$BASE/healthz" >/dev/null 2>&1 && break
  sleep 0.4
done
KEY="$("$CBX" api-key show | cut -f2)"
[ -n "$KEY" ] || fail "no API key"
ok "serving on $BASE"

echo "--- auth"
curl -sf "$BASE/healthz" | grep -q '"status":"ok"' || fail "healthz did not answer"
ok "healthz needs no key"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/sessions")" = "401" ] \
  || fail "an unauthenticated request was served"
ok "everything else refuses an unauthenticated request"
# A bare curl, not the helper: the helper already sends the valid key, and a
# second Authorization header does not replace the first.
WRONG="$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer cbx_live_definitely_wrong" "$BASE/sessions")"
[ "$WRONG" = "401" ] || fail "a wrong key was accepted ($WRONG)"
ok "a wrong key is refused"

echo "--- the API describes itself"
api GET /openapi.json | grep -q '"ClaudeBox API"' || fail "no OpenAPI document"
ok "openapi.json"
api GET /commands | grep -q '"name"' || fail "the command spec is not snake_case"
ok "commands, in the same shape PUT accepts"

echo "--- sessions"
api POST /sessions -d '{"name":"smoke","system_prompt":"Answer in as few words as possible."}' \
  | grep -q '"kind":"headless"' || fail "create did not make a headless session"
ok "create"
[ "$(code POST /sessions -d '{"name":"smoke"}')" = "409" ] || fail "a duplicate name was accepted"
ok "a duplicate name is refused"
[ "$(code POST /sessions -d '{"name":"bad","permission_mode":"yolo"}')" = "400" ] \
  || fail "an invalid permission mode was accepted"
[ "$(code POST /sessions -d '{"name":"bad","model":"--dangerously-skip-permissions"}')" = "400" ] \
  || fail "a model name that claude would read as a flag was accepted"
ok "invalid permission mode and flag-shaped model refused"

echo "--- a real query"
STAMP="report-$(date -u +%Y%m%dT%H%M%SZ).html"
ANSWER="$(api POST "/sessions/smoke/query" -d "$(cat <<JSON
{"prompt": "Write a file called $STAMP containing exactly <h1>ok</h1> and nothing else, then reply with just: DONE",
 "respond_within": "5m",
 "artifacts": ["$STAMP"]}
JSON
)")"
echo "$ANSWER" | grep -q '"status":"done"' || fail "the query did not finish: $ANSWER"
ok "claude -p answered over HTTP"

echo "--- artifacts"
api GET "/sessions/smoke/artifacts" | grep -q "$STAMP" || fail "the declared artifact was not registered"
ok "a declared artifact is registered"
api GET "/sessions/smoke/artifacts/$STAMP" | grep -q "<h1>ok</h1>" || fail "fetching it did not return the file"
ok "fetching it returns the file"
# The reason the register exists: a session directory holds things a caller
# has no business reading.
echo "SECRET" > "$HOME/workspace/smoke/.env" 2>/dev/null || true
[ "$(code GET "/sessions/smoke/artifacts/.env")" = "404" ] || fail "an undeclared file was fetchable"
[ "$(code GET "/sessions/smoke/artifacts/../../../etc/passwd")" = "404" ] \
  || fail "a traversal escaped the session"
ok "undeclared files and traversals are refused"

echo "--- /clear actually clears"
api POST "/sessions/smoke/query" \
  -d '{"prompt":"Remember the codeword SMOKETEST. Reply with just: OK","respond_within":"5m"}' >/dev/null
api POST "/sessions/smoke/query" \
  -d '{"prompt":"What is the codeword? One word.","respond_within":"5m"}' \
  | grep -q "SMOKETEST" || fail "the conversation did not remember before clearing"
api POST "/sessions/smoke/command" -d '{"command":"/clear"}' \
  | grep -q '"effect":"rotate-session"' || fail "/clear was not performed natively"
if api POST "/sessions/smoke/query" \
     -d '{"prompt":"What is the codeword? One word. If you do not know, say UNKNOWN.","respond_within":"5m"}' \
     | grep -q "SMOKETEST"; then
  fail "the codeword survived /clear — forwarding it does exactly this"
fi
ok "/clear forgets, where forwarding the command would not"

echo "--- an undeclared command"
[ "$(code POST "/sessions/smoke/command" -d '{"command":"/definitely-not-allowed"}')" = "400" ] \
  || fail "a command outside the allowlist was run"
ok "the allowlist denies by default"

echo "--- jobs"
JOB="$(api POST "/sessions/smoke/query" \
  -d '{"prompt":"Write a detailed 2000-word essay on the history of the bicycle.","respond_within":0}' \
  | sed -n 's/.*"job":"\([^"]*\)".*/\1/p')"
[ -n "$JOB" ] || fail "respond_within 0 did not return a job"
ok "respond_within 0 returns a job immediately"
api GET "/jobs/$JOB" | grep -q '"status":"running"' || fail "the job is not running"
sleep 3
api DELETE "/jobs/$JOB" | grep -q '"status":"cancelled"' || fail "the job was not cancelled"
ok "a running query can be cancelled"
# Cancelling costs the turn and nothing else.
api POST "/sessions/smoke/query" -d '{"prompt":"Reply with just: STILL HERE","respond_within":"5m"}' \
  | grep -q "STILL HERE" || fail "the session was unusable after a cancelled turn"
ok "the session still works after a cancelled turn"

echo "--- delete"
DIR="$(api GET /sessions/smoke | sed -n 's/.*"dir":"\([^"]*\)".*/\1/p')"
api DELETE /sessions/smoke >/dev/null
[ "$(code GET /sessions/smoke)" = "404" ] || fail "the session survived delete"
[ -d "$DIR" ] || fail "delete removed the working directory — it must not"
ok "delete forgets the session and keeps the work"
rm -rf "$DIR"

echo "--- key rotation"
NEW="$(api POST /auth/rotate | sed -n 's/.*"api_key":"\([^"]*\)".*/\1/p')"
[ -n "$NEW" ] || fail "rotate returned no key"
[ "$(code GET /sessions)" = "401" ] || fail "the old key still works"
KEY="$NEW"
[ "$(code GET /sessions)" = "200" ] || fail "the new key does not work"
ok "rotation issues a working key and kills the old one immediately"

echo "--- stop"
"$CBX" serve --stop >/dev/null
curl -sf -m 2 "$BASE/healthz" >/dev/null 2>&1 && fail "the server is still up"
ok "serve --stop"

echo
echo "PASS — the API drove a real Claude Code end to end"

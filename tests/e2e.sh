#!/bin/bash
# End-to-end test against a live ClaudeBox server.
# Usage: ./tests/e2e.sh <serve-tunnel-url> <api-key>
set -e

URL=${1:?"Usage: e2e.sh <serve-tunnel-url> <api-key>"}
KEY=${2:?"Usage: e2e.sh <serve-tunnel-url> <api-key>"}

echo "=== Health ==="
curl -sf "$URL/health" | jq .

echo ""
echo "=== Create Session ==="
curl -sf -X POST "$URL/sessions" \
  -H "X-API-Key: $KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "e2e-test"}' | jq .

echo ""
echo "=== List Sessions ==="
curl -sf "$URL/sessions" \
  -H "X-API-Key: $KEY" | jq .

echo ""
echo "=== Delete Session ==="
curl -sf -X DELETE "$URL/sessions/e2e-test" \
  -H "X-API-Key: $KEY" | jq .

echo ""
echo "=== Verify Deleted ==="
SESSIONS=$(curl -sf "$URL/sessions" -H "X-API-Key: $KEY")
if echo "$SESSIONS" | jq -e '.[] | select(.name == "e2e-test")' >/dev/null 2>&1; then
  echo "FAIL: session still exists"
  exit 1
fi
echo "OK: session deleted"

echo ""
echo "=== Auth Rejected ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$URL/sessions")
if [ "$STATUS" != "401" ]; then
  echo "FAIL: expected 401, got $STATUS"
  exit 1
fi
echo "OK: unauthorized returns 401"

echo ""
echo "PASS: all e2e tests passed"

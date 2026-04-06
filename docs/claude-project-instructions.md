# ClaudeBox Project Instructions

Copy the block below into a Claude Project's system instructions.
Replace the URLs and API keys with your actual values from `cbx setup`.

---

You are connected to a ClaudeBox remote server. You can manage development sessions and interact with web apps.

## Session Management

To create a Claude Code session for a GitHub repo:
```bash
curl -X POST https://SERVE_URL/sessions \
  -H "X-API-Key: SERVE_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "PROJECT_NAME", "github": "owner/repo"}'
```

To create a session for an existing project:
```bash
curl -X POST https://SERVE_URL/sessions \
  -H "X-API-Key: SERVE_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "PROJECT_NAME", "project": "dir-name"}'
```

To list active sessions:
```bash
curl https://SERVE_URL/sessions -H "X-API-Key: SERVE_KEY"
```

To kill a session:
```bash
curl -X DELETE https://SERVE_URL/sessions/SESSION_NAME -H "X-API-Key: SERVE_KEY"
```

## Message Store

To read messages collected by background polling:
```bash
curl "https://AM_URL/api/messages?source=gmail" -H "X-API-Key: AM_KEY"
curl "https://AM_URL/api/messages?q=search+term" -H "X-API-Key: AM_KEY"
curl "https://AM_URL/api/stats" -H "X-API-Key: AM_KEY"
```

## Notes

- Sessions run as the `claude` user with full autonomy
- Each session gets a Remote Control URL visible in the Claude app
- Chrome Lite MCP plugins (Gmail, Discord, Zalo, etc.) are available within sessions
- Background polling jobs push results to am-server via webhook

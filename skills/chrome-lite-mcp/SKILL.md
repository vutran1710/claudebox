---
name: chrome-lite-mcp
description: Use Chrome Lite MCP to read and interact with web apps (Gmail, Discord, Zalo, Messenger, Slack), then push messages to am-server for persistent storage and querying.
---

# Chrome Lite MCP + am-server

Chrome Lite MCP provides browser automation via MCP tools. Use it to read messages from web apps and push them to am-server.

## Chrome Lite MCP Tools

| Tool | Description |
|------|-------------|
| `tabs_list` | List all open Chrome tabs |
| `tab_create` | Open a new tab with URL |
| `tab_navigate` | Navigate a tab to a URL |
| `page_read` | Read page content (modes: text, interactive, accessibility) |
| `page_click` | Click element by CSS selector or coordinates |
| `page_type` | Type text into an element |
| `page_eval` | Execute JS via DevTools Protocol (bypasses CSP) |
| `page_screenshot` | Capture screenshot (base64 PNG) |

## Key Pattern: realClick

Gmail and other apps using custom event systems need full mouse event sequences:

```js
function realClick(el) {
  for (const type of ['mousedown', 'mouseup', 'click']) {
    el.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, view: window }));
  }
}
```

## Reading Messages

### Gmail

```js
// page_eval — list emails
(() => {
  const rows = document.querySelectorAll('tr.zA');
  const emails = [];
  rows.forEach((row, i) => {
    emails.push({
      index: i,
      unread: row.classList.contains('zE'),
      sender: (row.querySelector('.yW .zF, .yW .yP')?.getAttribute('name')) || '',
      subject: row.querySelector('.bog')?.textContent?.trim() || '',
      snippet: row.querySelector('.y2')?.textContent?.trim()?.slice(0, 100) || '',
    });
  });
  return emails;
})()
```

### Discord

```js
// page_eval — read chat messages
(() => {
  const msgs = document.querySelectorAll('[class*="message_"]');
  const results = [];
  msgs.forEach(m => {
    const author = m.querySelector('[class*="username_"]')?.textContent || '';
    const timestamp = m.querySelector('time')?.textContent || '';
    const content = m.querySelector('[class*="messageContent_"]')?.textContent || '';
    if (content || author) results.push({ author, timestamp, content });
  });
  return results.slice(-20);
})()
```

### Zalo

```js
// page_eval — read chat messages (after clicking into a conversation)
(() => {
  const msgs = document.querySelectorAll('.chat-message, .chat-message-v2');
  const results = [];
  msgs.forEach(m => {
    const sender = m.querySelector('.message-sender-name-content')?.textContent?.trim() || '';
    const text = m.querySelector('.text-message__container')?.textContent?.trim() || '';
    const isMe = !!m.closest('.message-wrapper--me');
    results.push({ sender: isMe ? 'Me' : (sender || 'same'), content: text || '[media]' });
  });
  return results.slice(-25);
})()
```

### Messenger

```js
// page_eval — read chat messages (non-E2EE chats only)
(() => {
  const rows = document.querySelectorAll('[role="row"]');
  const messages = [];
  rows.forEach(row => {
    const texts = row.querySelectorAll('[dir="auto"]');
    const content = Array.from(texts).map(t => t.textContent.trim()).filter(t => t.length > 0 && t.length < 500);
    if (content.length > 0) messages.push({ content: content.join(' | ') });
  });
  return messages.slice(-20);
})()
```

### Slack

```js
// page_eval — read channel messages
(() => {
  const msgs = document.querySelectorAll('[data-qa="message_container"]');
  const results = [];
  msgs.forEach(m => {
    const author = m.querySelector('[data-qa="message_sender_name"]')?.textContent || '';
    const content = m.querySelector('[data-qa="message-text"]')?.textContent || '';
    const time = m.querySelector('time')?.getAttribute('datetime') || '';
    if (content) results.push({ author, content, time });
  });
  return results.slice(-20);
})()
```

## Pushing to am-server

After reading messages, push them to am-server for persistent storage.

### am-server API

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/ingest` | Push messages (single or array) |
| GET | `/api/messages` | List/search messages |
| GET | `/api/messages/{id}` | Get single message |
| GET | `/api/stats` | Message counts by source |

**Auth:** `X-API-Key` header. Key is in `~/.agent-mesh/config.toml`.
**Server:** `http://localhost:8090`

### Message schema

```json
{
  "source": "gmail|discord|zalo|messenger|slack",
  "sender": "Display Name",
  "subject": "Email subject or chat/channel name",
  "preview": "First ~100 chars of message content",
  "raw": {},
  "source_ts": "2026-04-02T10:00:00Z"
}
```

### Ingest examples

```bash
# Push Gmail messages
curl -s -X POST http://localhost:8090/ingest \
  -H "X-API-Key: $AM_API_KEY" \
  -H "Content-Type: application/json" \
  -d '[
    {"source":"gmail","sender":"Vu Tran","subject":"CI failed","preview":"Rebuild Index failed..."},
    {"source":"gmail","sender":"Google","subject":"Security alert","preview":"New sign-in on Mac..."}
  ]'

# Push Discord messages
curl -s -X POST http://localhost:8090/ingest \
  -H "X-API-Key: $AM_API_KEY" \
  -H "Content-Type: application/json" \
  -d '[{"source":"discord","sender":"Bruno","subject":"DM","preview":"how are you"}]'

# Push Zalo messages
curl -s -X POST http://localhost:8090/ingest \
  -H "X-API-Key: $AM_API_KEY" \
  -H "Content-Type: application/json" \
  -d '[{"source":"zalo","sender":"Tuan Anh","subject":"1Chat","preview":"anh can rua 2-3tr..."}]'
```

### Query messages

```bash
# All messages from Gmail
curl -s "http://localhost:8090/api/messages?source=gmail" -H "X-API-Key: $AM_API_KEY"

# Search across all sources
curl -s "http://localhost:8090/api/messages?q=security+alert" -H "X-API-Key: $AM_API_KEY"

# Messages since a specific time
curl -s "http://localhost:8090/api/messages?since=2026-04-02T00:00:00Z" -H "X-API-Key: $AM_API_KEY"

# Stats by source
curl -s "http://localhost:8090/api/stats" -H "X-API-Key: $AM_API_KEY"
```

## Full Workflow: Check All Messages

```
1. Read AM_API_KEY from ~/.agent-mesh/config.toml

2. For each app:
   a. tab_navigate to the app URL
   b. page_eval with the app-specific JS snippet to extract messages
   c. POST results to http://localhost:8090/ingest

3. Summarize what's new for the user
```

## Replying to Messages

1. Navigate to the correct app and conversation
2. Find and click the message input
3. Use `document.execCommand('insertText', false, 'message')` via page_eval
4. Click send or press Enter

## Tips

- Use `page_read mode: "accessibility"` as fallback when selectors break
- Gmail toolbar buttons need `realClick()` — simple `click()` won't work
- Discord hashes class names — use `[class*="..."]` partial matches
- Messenger E2EE chats cannot be read (encrypted in DOM)
- Zalo requires QR code login — check if logged in first
- Always verify selectors with `page_read` if results are empty

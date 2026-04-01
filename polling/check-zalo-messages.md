Poll Zalo for new/unread messages using Chrome browser automation and forward them to am-server.

## Steps

1. Get tab context via `tabs_context_mcp`. If no tab exists, create one.
2. Navigate to `chat.zalo.me`.
3. Read the page (depth 5) and scan the conversation list for any entries with unread indicators (badge counts, bold text).
4. If no unread conversations found, stop silently — do nothing.
5. For each unread conversation (up to 10):
   a. Click the conversation to open it.
   b. Read the page to find recent messages. Collect messages that are new/unread — look for unread separators or use the badge count to determine how many recent messages to capture.
   c. Extract: sender name, message text, and timestamp.
6. POST collected messages to am-server as a JSON array:

```
curl -X POST http://localhost:8090/ingest \
  -H "X-API-Key: $AM_API_KEY" \
  -H "Content-Type: application/json" \
  -d '[
    {
      "source": "zalo",
      "sender": "<sender_name>",
      "subject": "DM",
      "preview": "<message_text>",
      "raw": {"text": "<message_text>", "channel": "<sender_name>", "type": "dm"},
      "source_ts": "<ISO8601_timestamp>"
    }
  ]'
```

7. Navigate back to the conversation list before finishing.

## Notes
- Use the `AM_API_KEY` environment variable for authentication.
- Each message is a separate entry in the array.
- `source_ts` must be ISO8601 format. Parse Zalo's displayed time into full ISO8601 using today's date as reference.
- If a message contains images/stickers/files, set preview to `[image]`, `[sticker]`, or `[file: <filename>]`.
- Do NOT forward your own messages. Only forward messages from others.
- Zalo may show a QR code login page — if so, stop and report the error, do not attempt to log in.
- For group chats with unread messages, set subject to the group name and sender to the individual message sender.

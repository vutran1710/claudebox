Poll Discord for new/unread DMs using Chrome browser automation and forward them to am-server.

## Steps

1. Get tab context via `tabs_context_mcp`. If no tab exists, create one.
2. Navigate to `discord.com/channels/@me`.
3. Read the page (depth 5) and scan the DM list for any entries with "unread" in their label/description.
4. If no unread DMs found, stop silently — do nothing.
5. For each unread DM:
   a. Click the DM link to open the conversation.
   b. Read the page to find the "new" separator (marks where unread messages begin).
   c. Collect all messages AFTER the "new" separator — extract sender name, message text, and timestamp.
6. POST collected messages to am-server as a JSON array:

```
curl -X POST http://localhost:8090/ingest \
  -H "X-API-Key: $AM_API_KEY" \
  -H "Content-Type: application/json" \
  -d '[
    {
      "source": "discord",
      "sender": "<sender_username>",
      "subject": "DM",
      "preview": "<message_text>",
      "raw": {"text": "<message_text>", "channel": "<sender_username>", "type": "dm"},
      "source_ts": "<ISO8601_timestamp>"
    }
  ]'
```

7. Navigate back to `discord.com/channels/@me` before finishing (so unread indicators reset).

## Notes
- Use the `AM_API_KEY` environment variable for authentication.
- Each message is a separate entry in the array.
- `source_ts` must be ISO8601 format (e.g. `2026-03-31T18:17:00+07:00`). Parse Discord's displayed time into this format.
- If a message contains images/embeds, set preview to `[image]` or `[embed: <title>]`.
- Do NOT forward your own messages (sender = current user). Only forward messages from others.

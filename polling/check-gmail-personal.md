Poll Gmail (personal account) for new/unread emails using Chrome browser automation and forward them to am-server.

## Prerequisites
- Switch to the **personal** Chrome profile before starting.

## Steps

1. Get tab context via `tabs_context_mcp`. If no tab exists, create one.
2. Navigate to `mail.google.com`.
3. Read the page (depth 5) and look for rows in the inbox with "unread" styling (bold text, unread indicators).
4. If no unread emails found, stop silently — do nothing.
5. For each unread email (up to 10 most recent):
   a. Extract from the inbox row: sender name, subject line, preview snippet, and timestamp.
   b. If needed, click the email to get the full preview text, then navigate back.
6. POST collected emails to am-server as a JSON array:

```
curl -X POST http://localhost:8090/ingest \
  -H "X-API-Key: $AM_API_KEY" \
  -H "Content-Type: application/json" \
  -d '[
    {
      "source": "gmail",
      "sender": "<sender_name> <sender_email>",
      "subject": "<email_subject>",
      "preview": "<snippet_or_first_lines>",
      "raw": {"from": "<sender_email>", "subject": "<email_subject>", "snippet": "<preview>", "labels": ["inbox", "unread"], "account": "personal"},
      "source_ts": "<ISO8601_timestamp>"
    }
  ]'
```

7. Do NOT mark emails as read — leave them in their current state.

## Notes
- Use the `AM_API_KEY` environment variable for authentication.
- Each email is a separate entry in the array.
- `source_ts` must be ISO8601 format. Parse Gmail's displayed time (e.g. "Mar 31", "2:07 PM") into full ISO8601 using today's date as reference.
- For emails showing only a date (no time), use 00:00 of that date.
- Skip promotional/spam tabs — only poll the Primary inbox.
- If Gmail shows a sign-in page, stop and report the error — do not attempt to log in.
- Tag all messages with `"account": "personal"` in the raw field.

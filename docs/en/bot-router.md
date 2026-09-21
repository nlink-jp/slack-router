# slack-bot-router — design notes (future work)

> **Status**: not implemented. A record of the design thinking and the specification.

---

## Overview

`slack-bot-router` is a daemon planned as a sibling of `slack-router`, to be implemented as a separate binary in the same repository.

Where `slack-router` is a **function dispatcher** for `/` slash commands, `slack-bot-router` is a **conversational interface** for `@bot` mentions. Their designs differ fundamentally, so they are kept as independent programs rather than merged.

---

## How its approach differs from slack-router

| | slack-router | slack-bot-router |
|---|---|---|
| Trigger | `/command` | `@bot message` |
| Approach | A function call (deterministic, idempotent) | A conversation with a virtual user |
| Responsible for replying | The worker POSTs to `response_url` | The router calls the Slack API |
| Bot Token | Not passed to the worker (not needed) | Held and used by the router (still not passed to the worker) |
| Replies from the worker | One, normally | Several (continuing until the process exits) |

---

## Architecture

```
[Slack]
  │  @bot message (app_mention)
  ▼
[slack-bot-router]          ← holds the Bot Token
  │  writes JSON to stdin once
  ▼
[worker script]             ← knows nothing about Slack
  │  writes JSON to stdout, one line at a time, several times
  │  exits 0 when done
  ▼
[slack-bot-router]
  │  posts each stdout line to the Slack API (chat.postMessage) in order
  ▼
[Slack]
```

### Why the worker becomes entirely independent of Slack

In `slack-router` the worker POSTs to `response_url` itself, so it has to know Slack's protocol. In `slack-bot-router` the router makes every Slack API call on the worker's behalf, so the worker only reads `stdin` and writes to `stdout`. The Bot Token does not reach the worker either.

---

## Worker protocol specification

### stdin (router → worker): once

```json
{
  "user_id":    "U123456",
  "channel_id": "C123456",
  "text":       "the message body with @bot removed",
  "event_ts":   "1234567890.123456",
  "thread_ts":  "1234567890.000000"
}
```

- `text`: the text with `<@BOTID>` stripped
- `event_ts`: the timestamp of the mention message
- `thread_ts`: the root TS to reply under when replying in a thread. For a mention outside a thread it is the same value as `event_ts`

### stdout (worker → router): zero or more times

**NDJSON (newline-delimited JSON). One line = one message.**

```jsonl
{"text": "Looking into it...", "response_type": "ephemeral"}
{"text": "Done!", "blocks": [...], "response_type": "in_channel"}
```

| Field | Required | Description |
|---|---|---|
| `text` | One of the two | A plain-text message |
| `blocks` | One of the two | An array of Slack Block Kit blocks. Can be used together with `text`, which then serves as the fallback |
| `response_type` | Optional | `"ephemeral"` (shown to the requesting user only) or `"in_channel"` (the default) |

The router passes the JSON fields from stdout to `chat.postMessage` as they are. Additional fields such as `thread_ts` are to be passed through as well.

### Exit signals

| Exit pattern | What the router does |
|---|---|
| `exit 0` | Normal exit. No notification |
| `exit N` (N > 0) | The worker exited deliberately. It is expected to have written its own error message to stdout. The router adds no notification |
| Killed by a signal (ExitCode < 0) | The worker exited without being able to notify. The router posts an error message |

> It inherits the same exit code conventions as `slack-router`.

---

## Implementation scope (first version)

### In scope
- Plain-text messages (`text`)
- Slack Block Kit (`blocks`)
- Threaded replies (`thread_ts`)
- Sending to stdout several times (progressive responses)
- Timeout and process group management (the same way as `slack-router`)
- Graceful shutdown

### Out of scope (for the first version)
- File attachments — `files.upload` takes several API steps, and there is no way to pass binary data over stdout, which does not fit a simple implementation
- Interactive components (button callbacks and the like)
- DMs (direct messages)
- Automatic management of conversation history (the worker implements it if it needs it)

---

## Implementation notes (for whoever implements this)

- `app_mention` events arrive through the Slack Events API. Under Socket Mode they are received as `EventTypeEventsAPI`; confirm that the inner event type is `app_mention` before dispatching
- When the router strips `<@BOTID>`, the Bot ID is obtained at startup with the `auth.test` API
- Read stdout line by line with `bufio.Scanner`, parse each line as JSON and post it immediately (do not wait for the process to exit)
- The `thread_ts` passed to `chat.postMessage` is `event_ts` (for a mention inside a thread, the original `thread_ts`)
- Command routing uses the first word after the mention. Design it so that a routing mismatch either falls back to a default route or returns an error

---

## Proposed layout in the same repository

```
slack-router/
├── main.go              ← slack-router (existing)
├── router.go
├── worker.go
├── ...
├── cmd/
│   └── bot-router/
│       ├── main.go      ← slack-bot-router (to be implemented)
│       └── ...
└── docs/
    ├── en/
    │   ├── slack-setup.md
    │   └── bot-router.md    ← this file
    └── ja/
        ├── slack-setup.ja.md
        └── bot-router.ja.md
```

It adopts the standard Go project layout for managing several binaries: keep `package main` and put the additional binaries under a `cmd/` subdirectory.

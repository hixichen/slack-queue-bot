# Slack Work Queue Bot — Implementation Plan

## TL;DR
Build a Slack bot that provides per-channel work/question queues using `!` prefix commands. Go + slack-go/slack (Socket Mode) + SQLite. Single binary, single container deployment. Estimated build time: 6-8 hours.

## Problem
Teams lose track of questions and work items in Slack channel noise. Need a lightweight, in-channel queue system where anyone can ask a question, items get assigned, and resolved items are tracked — all without leaving Slack.

## Commands

| Command | Description | Example |
|---------|-------------|---------|
| `!q <question>` | Add a question to the channel queue | `!q How do we rotate the DB creds?` |
| `!a <description>` | Add a work item (non-question) | `!a Update runbook for incident response` |
| `!l` | List all open items in this channel | `!l` |
| `!p <id> @user` | Assign an item to someone | `!p 3 @chen` |
| `!d <id>` | Mark an item as resolved | `!d 3` |
| `!d` *(in thread)* | Resolve the item whose thread you're replying in | *(reply to a `!q` message)* |

## Architecture

```
Slack Workspace
├── #security-intake    ──┐
├── #eng-oncall         ──┤── Slack Events API (Socket Mode)
└── #infra-requests     ──┘
         │
         ▼
┌─────────────────┐
│   Go Binary      │
│   slack-go/slack  │
│                  │
│   Socket Mode    │
│   (no public URL)│
└────────┬─────────┘
         │
         ▼
┌─────────────────┐
│   SQLite (WAL)   │
│   ./data/bot.db  │
└─────────────────┘
```

- **Socket Mode**: No public endpoint needed. Bot connects outbound to Slack via WebSocket.
- **SQLite WAL mode**: Allows concurrent reads while writing. Single file, zero config.
- **Per-channel isolation**: Every query scoped by `channel_id`.
- **Single binary**: Go compiles to one static binary — no runtime dependencies.

## Tech Stack
- **Language**: Go 1.25+
- **Slack SDK**: `github.com/slack-go/slack` (Socket Mode support)
- **Database**: `github.com/mattn/go-sqlite3` (CGo SQLite bindings)
- **Build**: `go build` → single binary (~15MB)
- **Deploy**: Docker container (single replica), distroless or alpine base

## SQLite Schema

```sql
CREATE TABLE IF NOT EXISTS items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  channel_id TEXT NOT NULL,
  type TEXT NOT NULL DEFAULT 'question',  -- 'question' | 'work'
  content TEXT NOT NULL,
  submitter_id TEXT NOT NULL,             -- Slack user ID
  assignee_id TEXT,                       -- Slack user ID, nullable
  status TEXT NOT NULL DEFAULT 'open',    -- 'open' | 'done'
  created_at DATETIME NOT NULL DEFAULT (datetime('now')),
  updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_items_channel_status ON items(channel_id, status);
```

## Project Structure

```
slack-queue-bot/
├── main.go                 # Entry point, Slack client init, socket mode loop
├── db.go                   # SQLite setup, migrations, CRUD functions
├── handlers.go             # Command routing + 5 command handlers
├── formatter.go            # Slack Block Kit message building
├── types.go                # Structs (Item, Config)
├── db_test.go              # DB layer tests
├── handlers_test.go        # Command parsing tests
├── data/                   # SQLite DB file (gitignored, volume-mounted)
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── go.sum
└── README.md
```

## Core Types

```go
type Item struct {
    ID          int64
    ChannelID   string
    Type        string // "question" | "work"
    Content     string
    SubmitterID string
    AssigneeID  *string // nullable
    Status      string  // "open" | "done"
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type DB struct {
    conn *sql.DB
}
```

## DB CRUD Functions

```go
func NewDB(path string) (*DB, error)              // Open + migrate
func (d *DB) AddItem(channelID, itemType, content, submitterID string) (int64, error)
func (d *DB) ListOpen(channelID string) ([]Item, error)
func (d *DB) AssignItem(id int64, channelID, assigneeID string) error
func (d *DB) DoneItem(id int64, channelID string) (*Item, error)
func (d *DB) GetItem(id int64, channelID string) (*Item, error)
```

Every mutation function takes `channelID` to enforce per-channel isolation.

## Command Details

### `!q <question>`
- Parse message text after `!q`
- Call `db.AddItem(channelID, "question", content, userID)`
- Reply: "✅ Question #<id> added by @user — <question>"

### `!l`
- Call `db.ListOpen(channelID)`
- Format as Block Kit section blocks
- If empty: "📭 No open items in this channel"

### `!a <description>`
- Same as `!q` but `type="work"`
- Reply: "✅ Work item #<id> added by @user"

### `!p <id> @user`
- Parse item ID and mentioned user ID
- Call `db.AssignItem(id, channelID, assigneeID)`
- Reply: "👤 Item #<id> assigned to @user"

### `!d <id>` *(or bare `!d` in a question thread)*
- Parse item ID, or resolve by thread timestamp when bare
- Call `db.DoneItem(id, channelID)` / `db.DoneItemByMsgTS(...)`
- Reply: "✅ Item #<id> resolved"

## Message Routing

```go
func handleMessage(evt *slackevents.MessageEvent, api *slack.Client, db *DB) {
    text := strings.TrimSpace(evt.Text)

    switch {
    case strings.HasPrefix(text, "!q "):
        handleQueue(evt, api, db, text[3:])
    case text == "!l":
        handleList(evt, api, db)
    case strings.HasPrefix(text, "!a "):
        handleAdd(evt, api, db, text[3:])
    case strings.HasPrefix(text, "!p "):
        handleAssign(evt, api, db, text[3:])
    case strings.HasPrefix(text, "!d "), text == "!d":
        handleDone(evt, api, db, text[2:])
    }
}
```

## Slack App Configuration

### Required Bot Token Scopes
- `channels:history` — read messages in public channels
- `groups:history` — read messages in private channels
- `chat:write` — post responses
- `users:read` — resolve user display names

### Event Subscriptions (Socket Mode)
- `message.channels` — public channel messages
- `message.groups` — private channel messages

### App-Level Token Scope
- `connections:write` — required for Socket Mode

### Environment Variables
```
SLACK_BOT_TOKEN=xoxb-...
SLACK_APP_TOKEN=xapp-...
DB_PATH=./data/bot.db
```

## Deployment

### Dockerfile
```dockerfile
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=1 go build -o queue-bot .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/queue-bot .
RUN mkdir -p /app/data
CMD ["./queue-bot"]
```

### Docker Compose (local dev)
```yaml
services:
  bot:
    build: .
    environment:
      - SLACK_BOT_TOKEN=${SLACK_BOT_TOKEN}
      - SLACK_APP_TOKEN=${SLACK_APP_TOKEN}
      - DB_PATH=/app/data/bot.db
    volumes:
      - ./data:/app/data
```

## Error Handling
- Invalid command syntax → reply with usage hint in channel
- Item not found → "❌ Item #<id> not found in this channel"
- Item already done → "⚠️ Item #<id> is already resolved"
- Missing content → "Usage: !q <your question>"
- Invalid user mention in assign → "Usage: !wassign <id> @user"
- DB errors → log internally, reply "⚠️ Something went wrong, try again"

## Build Sequence
1. Slack app setup (~30 min)
2. Project scaffold — go mod init, dependencies (~15 min)
3. Database layer — db.go + db_test.go (~1.5 hours)
4. Command handlers — handlers.go + all 5 handlers (~2-3 hours)
5. Formatter — formatter.go with Block Kit (~45 min)
6. Main + Socket Mode — main.go (~30 min)
7. Docker + deploy (~1 hour)
8. Testing (~30 min)

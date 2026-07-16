# slack-queue-bot

A lightweight, per-channel **work/question queue** for Slack, driven by `!` commands.

## What problem does it solve?

Questions and requests get lost in busy Slack channels. Someone asks "how do we
rotate the DB creds?", it scrolls away, nobody owns it, nobody closes it. This
bot gives every channel a tiny queue: anyone can file a question or work item,
assign it, and resolve it — without leaving Slack, without Jira-sized process.
Each channel's queue is fully isolated.

## Usage

| Command | Description | Example |
|---|---|---|
| `!q <question>` | Add a question to the channel queue | `!q How do we rotate creds?` |
| `!a <description>` | Add a work item | `!a Update the runbook` |
| `!l` | List open items in this channel | `!l` |
| `!p <id> @user` | Assign an item to someone | `!p 3 @chen` |
| `!d <id>` | Mark an item resolved | `!d 3` |
| `!d` *(in a thread)* | Resolve the item whose thread you replied in | *(reply `!d` under a `!q`)* |

```
!q How do we rotate the DB creds?   → ✅ Question #1 added by @you
!p 1 @oncall                        → 👤 Item #1 assigned to @oncall
!d 1                                → ✅ Item #1 resolved
```

Invalid input gets a usage hint; unknown or cross-channel IDs return a clear error.

## How it works

```
Slack workspace ──(Socket Mode WebSocket)──▶ Go binary ──▶ SQLite (WAL)
```

- **Socket Mode** — the bot dials out to Slack over a WebSocket; no public URL,
  no ingress, no inbound ports (besides an optional health endpoint).
- **SQLite (WAL)** — a single file, zero external dependencies, one writer.
- **Per-channel isolation** — every query is scoped by channel ID.
- **`/healthz`** on `:8080` returns `200` only while the Slack socket is
  connected, so orchestrator probes detect a dead connection.

## Setup

Create a Slack app at [api.slack.com/apps](https://api.slack.com/apps) → *From scratch*:

1. **Bot Token Scopes** (OAuth & Permissions): `channels:history`,
   `groups:history`, `chat:write`, `users:read`.
2. **Socket Mode** → enable, generate an **App-Level Token** with
   `connections:write` → this is `SLACK_APP_TOKEN` (`xapp-…`).
3. **Event Subscriptions** → subscribe to bot events `message.channels` and
   `message.groups`.
4. **Install to Workspace** → copy the **Bot User OAuth Token** →
   `SLACK_BOT_TOKEN` (`xoxb-…`), then `/invite @your-bot` into a channel.

| Env var | Default | Purpose |
|---|---|---|
| `SLACK_BOT_TOKEN` | *(required)* | Bot User OAuth token (`xoxb-…`) |
| `SLACK_APP_TOKEN` | *(required)* | App-level token for Socket Mode (`xapp-…`) |
| `DB_PATH` | `./data/bot.db` | SQLite file path |
| `HEALTH_ADDR` | `:8080` | Address for the `/healthz` server |

## Deployment

**Docker:**

```bash
docker run -d --name queue-bot \
  -e SLACK_BOT_TOKEN=xoxb-... \
  -e SLACK_APP_TOKEN=xapp-... \
  -e DB_PATH=/app/data/bot.db \
  -v queue-bot-data:/app/data \
  hixichen/slack-queue-bot:latest
```

**Kubernetes:** run a single replica with the `Recreate` strategy (SQLite has
one writer, Socket Mode holds one WebSocket), mount a PVC at `/app/data`, and
point liveness/readiness probes at `GET /healthz` on port 8080. Provide the two
tokens via a Secret. A complete manifest ships in the source repo.

The SQLite file lives on the volume and survives restarts. Tags: image versions
(`v0.0.1`, …) track the version constant in the source code; `latest` follows
the newest release.

## Security

- **Minimal runtime** — built on [Chainguard](https://images.chainguard.dev/)
  Wolfi images: a statically linked, CGO-free Go binary on
  `chainguard/static`. No shell, no package manager, near-zero CVE surface.
- **Non-root** — runs as the built-in `nonroot` user (UID 65532).
- **No inbound exposure** — Socket Mode dials out; the only listening port is
  the optional `/healthz` endpoint.
- **Secrets via environment** — tokens are read from env vars (Kubernetes
  Secret / compose env), never baked into the image or written to disk.
- **Scopes** — the `!`-prefix design requires the `*:history` scopes, i.e. the
  bot can read messages in channels it is invited to. Only queue items are
  stored; other messages are ignored and never persisted.

## Source code

GitHub: [hixichen/slack-queue-bot](https://github.com/hixichen/slack-queue-bot)
— issues and PRs welcome. Images are built from the tagged source; the image
tag matches the `version` constant in `main.go`.

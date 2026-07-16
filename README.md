# slack-queue-bot

A lightweight, in-channel **work/question queue** for Slack, driven by `!` commands.

Teams lose track of questions and work items in channel noise. This bot lets anyone
file a question or task, assign it, and resolve it — all without leaving Slack. Each
channel gets its own isolated queue.

```
!q How do we rotate the DB creds?     → ✅ Question #1 added by @you
!a Update the incident runbook         → ✅ Work item #2 added by @you
!l                                     → 📋 Open Items (2) …
!p 1 @oncall                           → 👤 Item #1 assigned to @oncall
!d 1                                   → ✅ Item #1 resolved
```

Single Go binary, Socket Mode (no public URL), SQLite storage. Runs as one small
container on Kubernetes.

---

## Commands

| Command | Description | Example |
|---|---|---|
| `!q <question>` | Add a question to the channel queue | `!q How do we rotate creds?` |
| `!a <description>` | Add a work item (non-question) | `!a Update the runbook` |
| `!l` | List all open items in this channel | `!l` |
| `!p <id> @user` | Assign an item to someone | `!p 3 @chen` |
| `!d <id>` | Mark an item resolved | `!d 3` |
| `!d` *(in a thread)* | Resolve the item whose thread you replied in | *(reply `!d` under a `!q`)* |

Invalid input gets a usage hint; unknown `id`s and cross-channel references return a
clear error. Queues are scoped per channel — `!d 3` only resolves item 3 in the
channel it was created in.

---

## How it works

```
Slack workspace ──(Socket Mode WebSocket)──▶ Go binary ──▶ SQLite (WAL)
   #channels                                  slack-go        ./data/bot.db
```

- **Socket Mode** — the bot connects outbound over a WebSocket; no inbound URL or
  ingress required.
- **SQLite (WAL)** — single file, zero external dependencies. One writer.
- **Per-channel isolation** — every query is scoped by `channel_id`.
- **`/healthz`** — HTTP endpoint on `:8080` that returns `200` only while the Slack
  socket is connected, so a dead connection is actually detected by k8s probes.

### Project layout

```
.
├── main.go                 # entry point: Slack client, socket loop, /healthz, graceful shutdown
├── pkg/bot/
│   ├── db.go               # SQLite setup, migrations, CRUD (channel-scoped)
│   ├── handlers.go         # command routing + handlers
│   ├── formatter.go        # Block Kit / message formatting
│   ├── types.go            # Item struct
│   ├── db_test.go          # storage layer tests
│   ├── handlers_test.go    # command/arg parsing tests
│   ├── formatter_test.go   # message rendering tests
│   └── integration_test.go # end-to-end command flows against a mock Slack API
└── deploy/
    ├── Dockerfile          # multi-stage, non-root runtime
    ├── docker-compose.yml  # local run
    └── k8s/                # Secret + Deployment (PVC, probes, single replica)
```

---

## Slack app setup

Create an app at [api.slack.com/apps](https://api.slack.com/apps) → **From scratch**.

**Bot Token Scopes** (OAuth & Permissions):

| Scope | Why |
|---|---|
| `channels:history` | read messages in public channels |
| `groups:history` | read messages in private channels |
| `chat:write` | post responses |
| `users:read` | resolve display names |

**Socket Mode** → enable it, then generate an **App-Level Token** with
`connections:write` (this is `SLACK_APP_TOKEN`, `xapp-…`).

**Event Subscriptions** → subscribe to bot events `message.channels` and
`message.groups`.

**Install to Workspace**, copy the **Bot User OAuth Token** (`SLACK_BOT_TOKEN`,
`xoxb-…`), then `/invite @your-bot` into a channel.

> Note: the `!`-prefix design requires `*:history` scopes, i.e. the bot reads all
> messages in channels it's in. That's inherent to prefix commands (slash commands
> would avoid it, at the cost of the `!` ergonomics).

---

## Run locally

```bash
export SLACK_BOT_TOKEN=xoxb-...
export SLACK_APP_TOKEN=xapp-...

make run            # builds and runs the binary
# or
make up             # docker compose (build + run)
```

Expected logs:

```
queue-bot ready
connected to Slack
```

### Configuration

| Env var | Default | Purpose |
|---|---|---|
| `SLACK_BOT_TOKEN` | *(required)* | Bot User OAuth token (`xoxb-…`) |
| `SLACK_APP_TOKEN` | *(required)* | App-level token for Socket Mode (`xapp-…`) |
| `DB_PATH` | `./data/bot.db` | SQLite file path |
| `HEALTH_ADDR` | `:8080` | address for the `/healthz` server |

---

## Test

```bash
make test           # go test ./... (unit + integration, no Slack workspace needed)
make lint           # go vet ./...
go test ./... -race -cover   # with race detector and coverage
```

The suite has three layers, none of which needs Slack credentials:

- **Unit tests** — command/ID parsing, message formatting, Block Kit rendering
  (`handlers_test.go`, `formatter_test.go`).
- **Storage tests** — every CRUD path against a real temp SQLite file, including
  channel isolation, already-done/not-found errors, and persistence across
  reopen (`db_test.go`).
- **Integration tests with a mock Slack API** — `slack-go` clients accept a
  custom base URL (`slack.OptionAPIURL`), so `integration_test.go` runs a local
  `httptest` server that impersonates `chat.postMessage`, then drives
  `HandleMessage` with synthetic message events. Each test asserts both what
  landed in SQLite and exactly what the bot posted back — channel, text,
  blocks, and `thread_ts` — covering every command, its error paths, thread
  resolution, and that bot/edited/non-command messages are ignored.

For a hands-on checklist against a real workspace, see [test.md](test.md).

---

## Build & push the image

The release version lives in one place: the `const version` in `main.go`. The
Makefile parses it and tags images `v<version>` plus `latest`, so the code,
image tags, and Docker Hub always agree. To cut a release, bump the constant
and run:

```bash
docker login

make docker-build && make docker-push      # build then push (v<version> + latest)
# or, from an arm64 host, build linux/amd64 and push in one step:
make image-release
```

Override `DOCKERHUB_REPO=...` or `TAG=...` to deviate from the defaults.

The image is built from [Chainguard](https://images.chainguard.dev/) Wolfi
bases: `chainguard/go` compiles a statically linked, CGO-free binary
(the SQLite driver is pure Go), and the runtime is `chainguard/static` — no
shell, no package manager, runs as the built-in non-root user (UID 65532).

---

## Deploy to Kubernetes

> **The Secret is a prerequisite and must be created out of band.** This repo does
> not ship or apply a Secret manifest — `make k8s-apply` only deploys the workload
> and will refuse to run until the Secret `slack-queue-bot` exists in the target
> namespace.

```bash
# 1. Create the Secret 'slack-queue-bot' (one-time, managed outside this repo).
#    It must contain keys SLACK_BOT_TOKEN and SLACK_APP_TOKEN.
kubectl create secret generic slack-queue-bot \
  --from-literal=SLACK_BOT_TOKEN=xoxb-... \
  --from-literal=SLACK_APP_TOKEN=xapp-...
# convenience equivalent, reading the tokens from your environment:
#   make k8s-secret

# 2. Deploy the workload (PVC + Deployment). Fails fast if the Secret is missing.
make k8s-apply

# 3. Verify
kubectl rollout status deployment/slack-queue-bot
kubectl logs -f deployment/slack-queue-bot
```

In a real environment the Secret is typically provisioned by your secrets tooling
(External Secrets Operator, Sealed Secrets, Vault, CI, etc.) rather than `kubectl
create` by hand. Either way, the deployment just references it by name.

If the Secret is absent, the pod stays in `CreateContainerConfigError` until you
create it — no redeploy needed once it exists.

Deployment notes:

- **Single replica, `Recreate` strategy** — correct for SQLite (one writer) and
  Socket Mode (one WebSocket). The old pod releases the `ReadWriteOnce` PVC before
  the new one starts, avoiding multi-attach errors on rollout.
- Runs as **non-root** (UID 65532, chainguard/static's nonroot user); `fsGroup`
  lets the pod write to the PVC.
- Liveness/readiness probe `GET /healthz` — readiness flips to not-ready the moment
  the socket drops; liveness tolerates transient reconnects (~90s before restart).
- If you pushed under a different repo/tag, update `image:` in
  `deploy/k8s/deployment.yaml`.

The SQLite file lives on the PVC and survives pod restarts. To wipe all data:

```bash
kubectl delete pvc slack-queue-bot-data
```

---

## Tech stack

- **Go 1.25**
- [`slack-go/slack`](https://github.com/slack-go/slack) — Socket Mode client
- [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) — pure-Go SQLite (no CGO)
- [Chainguard](https://images.chainguard.dev/) `go` + `static` images (multi-stage) + Kubernetes

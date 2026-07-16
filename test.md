# Testing the Slack Queue Bot

## 1. Create the Slack App

1. Go to [api.slack.com/apps](https://api.slack.com/apps) → **Create New App** → **From scratch**
2. Name it (e.g. `queue-bot`) and pick your workspace.

---

## 2. Configure Bot Token Scopes

**OAuth & Permissions → Scopes → Bot Token Scopes** — add:

| Scope | Why |
|---|---|
| `channels:history` | Read messages in public channels |
| `groups:history` | Read messages in private channels |
| `chat:write` | Post responses |
| `users:read` | Resolve display names |

---

## 3. Enable Socket Mode

1. **Settings → Socket Mode** → toggle **Enable Socket Mode**.
2. Under **App-Level Tokens** → **Generate Token** → name it `socketmode`, add scope `connections:write` → **Generate**.
3. Copy the `xapp-...` token — this is `SLACK_APP_TOKEN`.

---

## 4. Subscribe to Events

**Event Subscriptions → Enable Events** (Socket Mode means no URL needed).

Under **Subscribe to bot events**, add:
- `message.channels`
- `message.groups`

---

## 5. Install to Workspace

**OAuth & Permissions** → **Install to Workspace** → Allow.

Copy the **Bot User OAuth Token** (`xoxb-...`) — this is `SLACK_BOT_TOKEN`.

---

## 6. Run the Bot

```bash
# Option A — local binary
export SLACK_BOT_TOKEN=xoxb-...
export SLACK_APP_TOKEN=xapp-...
make run

# Option B — docker compose
SLACK_BOT_TOKEN=xoxb-... SLACK_APP_TOKEN=xapp-... make up
```

You should see:
```
queue-bot ready
connected to Slack
```

---

## 7. Invite the Bot to a Test Channel

```
/invite @queue-bot
```

---

## 8. Test Each Command

### Add a question
```
!q How do we rotate the DB creds?
```
Expected: `✅ Question #1 added by @you`

---

### Add a work item
```
!a Update the incident response runbook
```
Expected: `✅ Work item #2 added by @you`

---

### List the queue
```
!l
```
Expected: Block Kit list showing both items with type emoji, submitter, assignee, and age.

---

### Assign an item
```
!p 1 @yourname
```
Expected: `👤 Item #1 assigned to @yourname`

---

### Resolve by ID (from anywhere in the channel)
```
!d 2
```
Expected: `✅ Item #2 resolved`

---

### Resolve by thread *(the key new feature)*
1. Post `!q Why is prod slow?` — bot replies `✅ Question #3 added by @you`.
2. **Click "Reply in thread"** on that *original* `!q` message (not the bot reply).
3. Inside the thread, type:
   ```
   !d
   ```
Expected: bot replies in that same thread: `✅ Item #3 resolved`

---

### Empty queue
After resolving everything, `!l` shows:
```
📭 No open items in this channel.
```

---

## 9. Edge Cases to Verify

| Input | Expected |
|---|---|
| `!q` (no text) | `Usage: !q <your question>` |
| `!a` (no text) | `Usage: !a <description>` |
| `!d 999` | `❌ Item #999 not found in this channel.` |
| `!d 2` again (already done) | `⚠️ Item #2 is already resolved.` |
| `!p 1 @someone` on done item | `⚠️ Item #1 is already resolved.` |
| `!d` in channel (no thread) | `Usage: !d <id> — or reply !d inside a question thread` |
| `!d` in a non-queue thread | `❌ No queue item found for this thread.` |
| `!p abc @someone` | `Usage: !p <id> @user` |

---

## 10. Multi-Channel Isolation Check

1. Create a second test channel, invite the bot.
2. Add `!q Test in channel B` → gets ID `#3`.
3. In channel A, run `!d 3` → `❌ Item #3 not found in this channel.`

Confirms per-channel isolation is working.

---

## 11. Unit Tests (no Slack needed)

```bash
make test
```

Runs the full suite — storage tests against a temp SQLite file, parsing and
formatting unit tests, and end-to-end command flows against a mock Slack API
(`integration_test.go`). Expected tail of the output:

```
--- PASS: TestIntegrationDoneInThread     ← thread-based !d, end to end
--- PASS: TestIntegrationChannelIsolation
--- PASS: TestDoneItemByMsgTS
...
PASS
ok  github.com/chenxi/slack-queue-bot/pkg/bot
```

---

## 12. Build & Push the Image to Docker Hub

```bash
# Log in once
docker login

# Build and push. The tag comes from the `const version` in main.go
# (v<version> plus latest), so code and image versions stay aligned.
make docker-build
make docker-push

# Or build for linux/amd64 and push in one step (use this from an arm64 Mac):
make image-release

# Override the repo or tag if needed:
make image-release DOCKERHUB_REPO=you/your-repo TAG=custom
```

---

## 13. Deploy to Kubernetes

> The Secret is a **prerequisite** — it is NOT part of this repo and NOT applied by
> `make k8s-apply`. Create it first (one time / via your secrets tooling).

```bash
# 1. Create the prerequisite Secret 'slack-queue-bot' (keys: SLACK_BOT_TOKEN, SLACK_APP_TOKEN)
kubectl create secret generic slack-queue-bot \
  --from-literal=SLACK_BOT_TOKEN=xoxb-... \
  --from-literal=SLACK_APP_TOKEN=xapp-...
# or, with tokens already in your env:  make k8s-secret

# 2. Deploy the workload (PVC + Deployment) — fails fast if the Secret is missing
make k8s-apply

# 3. Verify
kubectl rollout status deployment/slack-queue-bot
kubectl logs -f deployment/slack-queue-bot
```

Notes:
- The Secret is provisioned out of band (by hand, or via External Secrets / Sealed
  Secrets / Vault / CI). The deployment only references it by name. If it is absent,
  the pod stays in `CreateContainerConfigError` until it exists.
- Single replica, `Recreate` strategy — correct for SQLite (one writer) and Socket
  Mode (one WebSocket). The old pod releases the RWO PVC before the new one starts.
- The pod runs as non-root (UID 65532); `fsGroup` lets it write to the PVC.
- Liveness/readiness hit `GET /healthz` on port 8080, which returns 200 only while
  the Slack socket is connected.
- If you pushed under a different repo/tag, update `image:` in
  `deploy/k8s/deployment.yaml` to match.

The SQLite file lives on the PVC and survives pod restarts.
To wipe all data: `kubectl delete pvc slack-queue-bot-data`.

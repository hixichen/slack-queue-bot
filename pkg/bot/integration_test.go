package bot

// Integration tests for the full command flow, with Slack mocked out.
//
// slack-go lets a client be pointed at any base URL via slack.OptionAPIURL,
// so these tests spin up a local httptest server that impersonates the Slack
// Web API (chat.postMessage), then drive HandleMessage with synthetic message
// events. Each test asserts on both sides of the flow: what ended up in
// SQLite, and exactly what the bot posted back to "Slack".

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

// postedMessage is one captured chat.postMessage call.
type postedMessage struct {
	Channel  string
	Text     string
	ThreadTS string
	Blocks   string // raw JSON of the blocks payload, if any
}

// fakeSlack impersonates the Slack Web API and records every posted message.
type fakeSlack struct {
	mu    sync.Mutex
	posts []postedMessage
}

func (f *fakeSlack) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.posts = append(f.posts, postedMessage{
		Channel:  r.FormValue("channel"),
		Text:     r.FormValue("text"),
		ThreadTS: r.FormValue("thread_ts"),
		Blocks:   r.FormValue("blocks"),
	})
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true,"channel":"C001","ts":"1700000099.000"}`)
}

// last returns the most recent captured message, failing the test if none.
func (f *fakeSlack) last(t *testing.T) postedMessage {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) == 0 {
		t.Fatal("expected the bot to post a message, but it posted nothing")
	}
	return f.posts[len(f.posts)-1]
}

func (f *fakeSlack) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.posts)
}

// newTestBot wires a fake Slack server, a real slack.Client pointed at it,
// and a fresh temp database.
func newTestBot(t *testing.T) (*fakeSlack, *slack.Client, *DB) {
	t.Helper()
	fake := &fakeSlack{}
	mux := http.NewServeMux()
	mux.HandleFunc("/chat.postMessage", fake.handlePostMessage)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	api := slack.New("xoxb-test-token", slack.OptionAPIURL(srv.URL+"/"))
	return fake, api, newTestDB(t)
}

// msg builds a synthetic channel message event.
func msg(user, channel, text, ts string) *slackevents.MessageEvent {
	return &slackevents.MessageEvent{User: user, Channel: channel, Text: text, TimeStamp: ts}
}

// threadMsg builds a synthetic thread-reply message event.
func threadMsg(user, channel, text, ts, threadTS string) *slackevents.MessageEvent {
	e := msg(user, channel, text, ts)
	e.ThreadTimeStamp = threadTS
	return e
}

func TestIntegrationAddQuestion(t *testing.T) {
	fake, api, db := newTestBot(t)

	HandleMessage(msg("U001", "C001", "!q How do we rotate creds?", "1700000001.000"), api, db)

	items, err := db.ListOpen("C001")
	if err != nil || len(items) != 1 {
		t.Fatalf("expected 1 open item, got %d (err=%v)", len(items), err)
	}
	if items[0].Type != "question" || items[0].Content != "How do we rotate creds?" ||
		items[0].SubmitterID != "U001" || items[0].MsgTS != "1700000001.000" {
		t.Errorf("unexpected item: %+v", items[0])
	}

	post := fake.last(t)
	if post.Channel != "C001" {
		t.Errorf("posted to %q, want C001", post.Channel)
	}
	if !strings.Contains(post.Text, fmt.Sprintf("Question *#%d*", items[0].ID)) ||
		!strings.Contains(post.Text, "<@U001>") {
		t.Errorf("unexpected confirmation: %q", post.Text)
	}
}

func TestIntegrationAddWorkItem(t *testing.T) {
	fake, api, db := newTestBot(t)

	HandleMessage(msg("U002", "C001", "!a Update the runbook", "1700000002.000"), api, db)

	items, _ := db.ListOpen("C001")
	if len(items) != 1 || items[0].Type != "work" {
		t.Fatalf("expected 1 work item, got %+v", items)
	}
	if !strings.Contains(fake.last(t).Text, "Work item") {
		t.Errorf("unexpected confirmation: %q", fake.last(t).Text)
	}
}

func TestIntegrationEmptyCommandShowsUsage(t *testing.T) {
	for _, text := range []string{"!q", "!a", "!p", "!d"} {
		t.Run(text, func(t *testing.T) {
			fake, api, db := newTestBot(t)
			HandleMessage(msg("U001", "C001", text, "1700000003.000"), api, db)
			if got := fake.last(t).Text; !strings.Contains(got, "Usage:") {
				t.Errorf("%s: got %q, want usage hint", text, got)
			}
			if items, _ := db.ListOpen("C001"); len(items) != 0 {
				t.Errorf("%s: no item should have been created", text)
			}
		})
	}
}

func TestIntegrationList(t *testing.T) {
	fake, api, db := newTestBot(t)
	HandleMessage(msg("U001", "C001", "!q first question", "1700000004.000"), api, db)
	HandleMessage(msg("U002", "C001", "!a second task", "1700000005.000"), api, db)

	HandleMessage(msg("U001", "C001", "!l", "1700000006.000"), api, db)

	post := fake.last(t)
	if post.Blocks == "" {
		t.Fatal("!l should post Block Kit blocks")
	}
	for _, want := range []string{"Open Items (2)", "first question", "second task"} {
		if !strings.Contains(post.Blocks, want) {
			t.Errorf("!l blocks missing %q", want)
		}
	}
	// The zero-time bug regression: no item is minutes old, so ages must render
	// as "just now", never as day counts.
	if strings.Contains(post.Blocks, "d ago") {
		t.Errorf("!l reports a stale age for a fresh item: %s", post.Blocks)
	}
}

func TestIntegrationListEmpty(t *testing.T) {
	fake, api, db := newTestBot(t)
	HandleMessage(msg("U001", "C001", "!l", "1700000007.000"), api, db)
	if !strings.Contains(fake.last(t).Blocks, "No open items") {
		t.Errorf("empty !l blocks: %s", fake.last(t).Blocks)
	}
}

func TestIntegrationAssign(t *testing.T) {
	fake, api, db := newTestBot(t)
	HandleMessage(msg("U001", "C001", "!q who owns this?", "1700000008.000"), api, db)
	id := db.mustLastID(t, "C001")

	HandleMessage(msg("U001", "C001", fmt.Sprintf("!p %d <@U002>", id), "1700000009.000"), api, db)

	item, _ := db.GetItem(id, "C001")
	if item.AssigneeID == nil || *item.AssigneeID != "U002" {
		t.Fatalf("expected assignee U002, got %v", item.AssigneeID)
	}
	if got := fake.last(t).Text; !strings.Contains(got, "<@U002>") {
		t.Errorf("unexpected assign confirmation: %q", got)
	}
}

func TestIntegrationAssignErrors(t *testing.T) {
	cases := []struct {
		name, cmd, want string
	}{
		{"unknown id", "!p 999 <@U002>", "not found"},
		{"bad id", "!p abc <@U002>", "Usage:"},
		{"zero id", "!p 0 <@U002>", "Usage:"},
		{"no mention", "!p 1 someone", "Usage:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake, api, db := newTestBot(t)
			HandleMessage(msg("U001", "C001", "!q q", "1700000010.000"), api, db)
			HandleMessage(msg("U001", "C001", c.cmd, "1700000011.000"), api, db)
			if got := fake.last(t).Text; !strings.Contains(got, c.want) {
				t.Errorf("%s: got %q, want substring %q", c.cmd, got, c.want)
			}
		})
	}
}

func TestIntegrationDoneByID(t *testing.T) {
	fake, api, db := newTestBot(t)
	HandleMessage(msg("U001", "C001", "!q done me", "1700000012.000"), api, db)
	id := db.mustLastID(t, "C001")

	HandleMessage(msg("U001", "C001", fmt.Sprintf("!d %d", id), "1700000013.000"), api, db)

	if items, _ := db.ListOpen("C001"); len(items) != 0 {
		t.Fatalf("expected no open items, got %d", len(items))
	}
	if got := fake.last(t).Text; !strings.Contains(got, "resolved") {
		t.Errorf("unexpected done confirmation: %q", got)
	}

	// Doing it again reports "already resolved".
	HandleMessage(msg("U001", "C001", fmt.Sprintf("!d %d", id), "1700000014.000"), api, db)
	if got := fake.last(t).Text; !strings.Contains(got, "already resolved") {
		t.Errorf("second !d: got %q, want already-resolved warning", got)
	}
}

func TestIntegrationDoneUnknownID(t *testing.T) {
	fake, api, db := newTestBot(t)
	HandleMessage(msg("U001", "C001", "!d 999", "1700000015.000"), api, db)
	if got := fake.last(t).Text; !strings.Contains(got, "not found") {
		t.Errorf("got %q, want not-found error", got)
	}
}

func TestIntegrationDoneInThread(t *testing.T) {
	fake, api, db := newTestBot(t)
	const parentTS = "1700000016.000"
	HandleMessage(msg("U001", "C001", "!q why is prod slow?", parentTS), api, db)

	// Bare !d replied inside the thread of the original !q message.
	HandleMessage(threadMsg("U002", "C001", "!d", "1700000017.000", parentTS), api, db)

	if items, _ := db.ListOpen("C001"); len(items) != 0 {
		t.Fatalf("thread !d did not resolve the item")
	}
	post := fake.last(t)
	if !strings.Contains(post.Text, "resolved") {
		t.Errorf("unexpected reply: %q", post.Text)
	}
	if post.ThreadTS != parentTS {
		t.Errorf("reply should stay in the thread: thread_ts=%q, want %q", post.ThreadTS, parentTS)
	}
}

func TestIntegrationDoneInUnrelatedThread(t *testing.T) {
	fake, api, db := newTestBot(t)
	HandleMessage(threadMsg("U001", "C001", "!d", "1700000018.000", "1690000000.000"), api, db)
	if got := fake.last(t).Text; !strings.Contains(got, "No queue item found") {
		t.Errorf("got %q, want no-queue-item error", got)
	}
}

func TestIntegrationChannelIsolation(t *testing.T) {
	fake, api, db := newTestBot(t)
	HandleMessage(msg("U001", "C001", "!q only in C001", "1700000019.000"), api, db)
	id := db.mustLastID(t, "C001")

	// Resolving from another channel must fail and leave the item open.
	HandleMessage(msg("U001", "C999", fmt.Sprintf("!d %d", id), "1700000020.000"), api, db)
	if got := fake.last(t).Text; !strings.Contains(got, "not found") {
		t.Errorf("cross-channel !d: got %q, want not-found error", got)
	}
	if items, _ := db.ListOpen("C001"); len(items) != 1 {
		t.Errorf("cross-channel !d must not resolve the item")
	}
}

func TestIntegrationIgnoresBotsEditsAndChatter(t *testing.T) {
	fake, api, db := newTestBot(t)

	// Bot messages, edited messages, and non-command chatter must be ignored.
	bot := msg("U001", "C001", "!q from a bot", "1700000021.000")
	bot.BotID = "B001"
	HandleMessage(bot, api, db)

	edited := msg("U001", "C001", "!q edited", "1700000022.000")
	edited.SubType = "message_changed"
	HandleMessage(edited, api, db)

	HandleMessage(msg("U001", "C001", "just talking about !q here", "1700000023.000"), api, db)
	HandleMessage(msg("U001", "C001", "!unknown command", "1700000024.000"), api, db)

	if n := fake.count(); n != 0 {
		t.Errorf("expected no posts, got %d: %+v", n, fake.posts)
	}
	if items, _ := db.ListOpen("C001"); len(items) != 0 {
		t.Errorf("expected no items, got %d", len(items))
	}
}

// mustLastID returns the ID of the only open item in the channel.
func (d *DB) mustLastID(t *testing.T, channelID string) int64 {
	t.Helper()
	items, err := d.ListOpen(channelID)
	if err != nil || len(items) == 0 {
		t.Fatalf("expected an open item in %s (err=%v)", channelID, err)
	}
	return items[len(items)-1].ID
}

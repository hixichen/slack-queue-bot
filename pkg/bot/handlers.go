package bot

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

// userMentionRe matches a Slack user mention like <@U12345678>.
var userMentionRe = regexp.MustCompile(`<@([A-Z0-9]+)>`)

// HandleMessage routes incoming message events to the appropriate command handler.
//
// Commands:
//
//	!q <text>    — add a question
//	!a <text>    — add a work item
//	!l           — list open items
//	!p <id> @u   — assign item to user
//	!d <id>      — mark item done (or bare !d inside a question thread)
func HandleMessage(evt *slackevents.MessageEvent, api *slack.Client, db *DB) {
	if evt.BotID != "" || evt.SubType != "" {
		return
	}

	text := strings.TrimSpace(evt.Text)

	switch {
	case strings.HasPrefix(text, "!q "):
		handleQueue(evt, api, db, strings.TrimSpace(text[3:]))
	case text == "!q":
		reply(api, evt.Channel, usage("!q"))

	case strings.HasPrefix(text, "!a "):
		handleAdd(evt, api, db, strings.TrimSpace(text[3:]))
	case text == "!a":
		reply(api, evt.Channel, usage("!a"))

	case text == "!l":
		handleList(evt, api, db)

	case strings.HasPrefix(text, "!p "):
		handleAssign(evt, api, db, strings.TrimSpace(text[3:]))
	case text == "!p":
		reply(api, evt.Channel, usage("!p"))

	case strings.HasPrefix(text, "!d "):
		handleDone(evt, api, db, strings.TrimSpace(text[3:]))
	case text == "!d":
		// Bare !d: resolve by thread context if in a thread, else show usage.
		handleDone(evt, api, db, "")
	}
}

func handleQueue(evt *slackevents.MessageEvent, api *slack.Client, db *DB, content string) {
	if content == "" {
		reply(api, evt.Channel, usage("!q"))
		return
	}
	id, err := db.AddItem(evt.Channel, "question", content, evt.User, evt.TimeStamp)
	if err != nil {
		log.Printf("AddItem error: %v", err)
		reply(api, evt.Channel, FormatWarning("Something went wrong, try again."))
		return
	}
	reply(api, evt.Channel, FormatAdded("question", id, evt.User))
}

func handleAdd(evt *slackevents.MessageEvent, api *slack.Client, db *DB, content string) {
	if content == "" {
		reply(api, evt.Channel, usage("!a"))
		return
	}
	id, err := db.AddItem(evt.Channel, "work", content, evt.User, evt.TimeStamp)
	if err != nil {
		log.Printf("AddItem error: %v", err)
		reply(api, evt.Channel, FormatWarning("Something went wrong, try again."))
		return
	}
	reply(api, evt.Channel, FormatAdded("work", id, evt.User))
}

func handleList(evt *slackevents.MessageEvent, api *slack.Client, db *DB) {
	items, err := db.ListOpen(evt.Channel)
	if err != nil {
		log.Printf("ListOpen error: %v", err)
		reply(api, evt.Channel, FormatWarning("Something went wrong, try again."))
		return
	}
	_, _, err = api.PostMessage(evt.Channel, slack.MsgOptionBlocks(FormatList(items)...))
	if err != nil {
		log.Printf("PostMessage error: %v", err)
	}
}

func handleAssign(evt *slackevents.MessageEvent, api *slack.Client, db *DB, args string) {
	id, assigneeID, ok := parseAssignArgs(args)
	if !ok {
		reply(api, evt.Channel, usage("!p"))
		return
	}
	if err := db.AssignItem(id, evt.Channel, assigneeID); err != nil {
		reply(api, evt.Channel, itemErrMsg(id, err))
		return
	}
	reply(api, evt.Channel, FormatAssigned(id, assigneeID))
}

// handleDone handles !d.
//
// Mode 1 — explicit ID:  `!d <id>` (works anywhere)
// Mode 2 — thread reply: bare `!d` inside the thread of a !q / !a message
func handleDone(evt *slackevents.MessageEvent, api *slack.Client, db *DB, args string) {
	if args != "" {
		id, err := parseID(args)
		if err != nil {
			reply(api, evt.Channel, usage("!d"))
			return
		}
		item, err := db.DoneItem(id, evt.Channel)
		if err != nil {
			reply(api, evt.Channel, itemErrMsg(id, err))
			return
		}
		replyInThread(api, evt.Channel, evt.ThreadTimeStamp, FormatDone(item))
		return
	}

	// Mode 2: must be a thread reply whose parent is a queue item.
	if evt.ThreadTimeStamp == "" {
		reply(api, evt.Channel, usage("!d"))
		return
	}
	item, err := db.DoneItemByMsgTS(evt.Channel, evt.ThreadTimeStamp)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			replyInThread(api, evt.Channel, evt.ThreadTimeStamp,
				FormatError("No queue item found for this thread."))
		case errors.Is(err, ErrAlreadyDone):
			replyInThread(api, evt.Channel, evt.ThreadTimeStamp,
				FormatWarning("This item is already resolved."))
		default:
			log.Printf("DoneItemByMsgTS error: %v", err)
			replyInThread(api, evt.Channel, evt.ThreadTimeStamp,
				FormatWarning("Something went wrong, try again."))
		}
		return
	}
	replyInThread(api, evt.Channel, evt.ThreadTimeStamp, FormatDone(item))
}

// --- helpers ---

func reply(api *slack.Client, channelID, text string) {
	if _, _, err := api.PostMessage(channelID, slack.MsgOptionText(text, false)); err != nil {
		log.Printf("PostMessage error: %v", err)
	}
}

func replyInThread(api *slack.Client, channelID, threadTS, text string) {
	opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}
	if _, _, err := api.PostMessage(channelID, opts...); err != nil {
		log.Printf("PostMessage error: %v", err)
	}
}

// parseID parses a positive item ID. Item IDs are SQLite autoincrement values
// starting at 1, so zero and negatives are rejected as malformed.
func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, err
	}
	if id <= 0 {
		return 0, fmt.Errorf("id must be positive: %d", id)
	}
	return id, nil
}

func parseAssignArgs(args string) (int64, string, bool) {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		return 0, "", false
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	m := userMentionRe.FindStringSubmatch(strings.Join(parts[1:], " "))
	if m == nil {
		return 0, "", false
	}
	return id, m[1], true
}

func itemErrMsg(id int64, err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return FormatError(fmt.Sprintf("Item #%d not found in this channel.", id))
	case errors.Is(err, ErrAlreadyDone):
		return FormatWarning(fmt.Sprintf("Item #%d is already resolved.", id))
	default:
		log.Printf("item op error id=%d: %v", id, err)
		return FormatWarning("Something went wrong, try again.")
	}
}

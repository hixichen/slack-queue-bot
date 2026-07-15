package bot

import (
	"fmt"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

const maxContentLen = 80

// maxListItems caps how many items FormatList renders. Slack rejects messages
// with more than 50 blocks; header + divider + optional footer leave room for 47.
const maxListItems = 47

// truncate shortens s to at most n runes, appending an ellipsis. It counts
// runes (not bytes) so multi-byte content (emoji, non-ASCII) is never cut
// mid-character.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func typeEmoji(itemType string) string {
	if itemType == "question" {
		return "❓"
	}
	return "🔧"
}

func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// FormatList builds a Block Kit message for the open items list.
func FormatList(items []Item) []slack.Block {
	if len(items) == 0 {
		return []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", "📭 No open items in this channel.", false, false),
				nil, nil,
			),
		}
	}

	blocks := []slack.Block{
		slack.NewHeaderBlock(
			slack.NewTextBlockObject("plain_text", fmt.Sprintf("📋 Open Items (%d)", len(items)), false, false),
		),
		slack.NewDividerBlock(),
	}

	overflow := 0
	if len(items) > maxListItems {
		overflow = len(items) - maxListItems
		items = items[:maxListItems]
	}

	for _, it := range items {
		assignee := "_unassigned_"
		if it.AssigneeID != nil {
			assignee = fmt.Sprintf("<@%s>", *it.AssigneeID)
		}
		text := fmt.Sprintf(
			"%s *#%d* — %s\n<@%s> · assignee: %s · %s",
			typeEmoji(it.Type),
			it.ID,
			truncate(it.Content, maxContentLen),
			it.SubmitterID,
			assignee,
			age(it.CreatedAt),
		)
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", text, false, false),
			nil, nil,
		))
	}

	if overflow > 0 {
		blocks = append(blocks, slack.NewContextBlock("",
			slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("…and %d more. Resolve some with `!d <id>`.", overflow), false, false),
		))
	}

	return blocks
}

// FormatAdded returns a confirmation message for a newly added item.
func FormatAdded(itemType string, id int64, submitterID string) string {
	kind := "Question"
	if itemType == "work" {
		kind = "Work item"
	}
	return fmt.Sprintf("✅ %s *#%d* added by <@%s>", kind, id, submitterID)
}

// FormatAssigned returns a confirmation message for assignment.
func FormatAssigned(id int64, assigneeID string) string {
	return fmt.Sprintf("👤 Item *#%d* assigned to <@%s>", id, assigneeID)
}

// FormatDone returns a confirmation message for resolving an item.
func FormatDone(item *Item) string {
	return fmt.Sprintf("✅ Item *#%d* resolved", item.ID)
}

// FormatError returns a user-visible error message.
func FormatError(msg string) string {
	return "❌ " + msg
}

// FormatWarning returns a user-visible warning message.
func FormatWarning(msg string) string {
	return "⚠️ " + msg
}

// usage returns a short usage hint for a command.
func usage(cmd string) string {
	hints := map[string]string{
		"!q": "Usage: `!q <your question>`",
		"!a": "Usage: `!a <description>`",
		"!l": "Usage: `!l`",
		"!p": "Usage: `!p <id> @user`",
		"!d": "Usage: `!d <id>` — or reply `!d` inside a question thread",
	}
	if h, ok := hints[cmd]; ok {
		return h
	}
	return fmt.Sprintf("Unknown command: %s", strings.TrimSpace(cmd))
}

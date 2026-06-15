package bot

import "time"

// Item represents a work item or question in the queue.
type Item struct {
	ID          int64
	ChannelID   string
	Type        string // "question" | "work"
	Content     string
	SubmitterID string
	AssigneeID  *string // nullable Slack user ID
	Status      string  // "open" | "done"
	MsgTS       string  // Slack message timestamp of the original !q / !wadd message
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

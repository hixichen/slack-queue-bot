package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"
)

// blocksJSON marshals blocks so tests can assert on rendered content.
// HTML escaping is off so mentions like <@U001> stay readable.
func blocksJSON(t *testing.T, blocks []slack.Block) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(blocks); err != nil {
		t.Fatalf("marshal blocks: %v", err)
	}
	return buf.String()
}

func TestFormatListEmpty(t *testing.T) {
	blocks := FormatList(nil)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block for empty list, got %d", len(blocks))
	}
	if !strings.Contains(blocksJSON(t, blocks), "No open items") {
		t.Error("empty list should say there are no open items")
	}
}

func TestFormatListContent(t *testing.T) {
	assignee := "U002"
	items := []Item{
		{ID: 1, Type: "question", Content: "How do we rotate creds?", SubmitterID: "U001", CreatedAt: time.Now()},
		{ID: 2, Type: "work", Content: "Update runbook", SubmitterID: "U001", AssigneeID: &assignee, CreatedAt: time.Now()},
	}
	blocks := FormatList(items)

	// header + divider + one section per item
	if len(blocks) != 4 {
		t.Fatalf("expected 4 blocks, got %d", len(blocks))
	}
	got := blocksJSON(t, blocks)
	for _, want := range []string{
		"Open Items (2)",
		"#1", "How do we rotate creds?", "❓",
		"#2", "Update runbook", "🔧",
		"<@U001>", "<@U002>", "_unassigned_",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatList output missing %q", want)
		}
	}
}

func TestFormatListTruncatesLongContent(t *testing.T) {
	long := strings.Repeat("x", 300)
	blocks := FormatList([]Item{{ID: 1, Type: "work", Content: long, SubmitterID: "U001", CreatedAt: time.Now()}})
	got := blocksJSON(t, blocks)
	if strings.Contains(got, long) {
		t.Error("long content should be truncated")
	}
	if !strings.Contains(got, "…") {
		t.Error("truncated content should end with ellipsis")
	}
}

func TestFormatListCapsBlockCount(t *testing.T) {
	var items []Item
	for i := 1; i <= 120; i++ {
		items = append(items, Item{
			ID: int64(i), Type: "work", Content: fmt.Sprintf("task %d", i),
			SubmitterID: "U001", CreatedAt: time.Now(),
		})
	}
	blocks := FormatList(items)
	// Slack rejects messages with more than 50 blocks.
	if len(blocks) > 50 {
		t.Fatalf("FormatList produced %d blocks; Slack allows at most 50", len(blocks))
	}
	got := blocksJSON(t, blocks)
	if !strings.Contains(got, fmt.Sprintf("and %d more", 120-maxListItems)) {
		t.Error("capped list should mention how many items were omitted")
	}
	if !strings.Contains(got, "Open Items (120)") {
		t.Error("header should still show the full count")
	}
}

func TestAge(t *testing.T) {
	now := time.Now()
	cases := []struct {
		t    time.Time
		want string
	}{
		{now.Add(-10 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-49 * time.Hour), "2d ago"},
	}
	for _, c := range cases {
		if got := age(c.t); got != c.want {
			t.Errorf("age(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestFormatAssigned(t *testing.T) {
	got := FormatAssigned(3, "U123")
	if !strings.Contains(got, "#3") || !strings.Contains(got, "<@U123>") {
		t.Errorf("FormatAssigned = %q, want item id and mention", got)
	}
}

func TestFormatDone(t *testing.T) {
	got := FormatDone(&Item{ID: 7, Content: "secret content"})
	if !strings.Contains(got, "#7") {
		t.Errorf("FormatDone = %q, want #7", got)
	}
	if strings.Contains(got, "secret content") {
		t.Errorf("FormatDone should not echo content, got %q", got)
	}
}

func TestFormatErrorAndWarning(t *testing.T) {
	if got := FormatError("nope"); !strings.HasPrefix(got, "❌") || !strings.Contains(got, "nope") {
		t.Errorf("FormatError = %q", got)
	}
	if got := FormatWarning("careful"); !strings.HasPrefix(got, "⚠️") || !strings.Contains(got, "careful") {
		t.Errorf("FormatWarning = %q", got)
	}
}

func TestUsage(t *testing.T) {
	for _, cmd := range []string{"!q", "!a", "!l", "!p", "!d"} {
		if got := usage(cmd); !strings.Contains(got, "Usage:") {
			t.Errorf("usage(%q) = %q, want a usage hint", cmd, got)
		}
	}
	if got := usage("!zzz"); !strings.Contains(got, "Unknown command") {
		t.Errorf("usage(unknown) = %q", got)
	}
}

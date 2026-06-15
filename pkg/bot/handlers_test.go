package bot

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseID(t *testing.T) {
	cases := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		{"3", 3, false},
		{"  42  ", 42, false},
		{"0", 0, true},
		{"abc", 0, true},
		{"", 0, true},
		{"-1", 0, true},
	}
	for _, c := range cases {
		got, err := parseID(c.input)
		if (err != nil) != c.wantErr {
			t.Errorf("parseID(%q) error=%v, wantErr=%v", c.input, err, c.wantErr)
		}
		if err == nil && got != c.want {
			t.Errorf("parseID(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestParseAssignArgs(t *testing.T) {
	cases := []struct {
		input    string
		wantID   int64
		wantUser string
		wantOK   bool
	}{
		{"3 <@U123ABC>", 3, "U123ABC", true},
		{"  10  <@UAAA111>  ", 10, "UAAA111", true},
		{"3", 0, "", false},           // missing user
		{"abc <@U123>", 0, "", false}, // bad ID
		{"", 0, "", false},
		{"3 @notaslackid", 0, "", false}, // not a proper mention
	}
	for _, c := range cases {
		id, user, ok := parseAssignArgs(c.input)
		if ok != c.wantOK {
			t.Errorf("parseAssignArgs(%q) ok=%v, want %v", c.input, ok, c.wantOK)
			continue
		}
		if ok {
			if id != c.wantID {
				t.Errorf("parseAssignArgs(%q) id=%d, want %d", c.input, id, c.wantID)
			}
			if user != c.wantUser {
				t.Errorf("parseAssignArgs(%q) user=%q, want %q", c.input, user, c.wantUser)
			}
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hell…"},
		{"abc", 3, "abc"},
		{"abcd", 3, "ab…"},
		{"héllo wörld", 5, "héll…"}, // multi-byte runes counted, not bytes
		{"👍👍👍👍", 3, "👍👍…"},          // emoji never cut mid-rune
		{"héllo", 10, "héllo"},
	}
	for _, c := range cases {
		got := truncate(c.input, c.n)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.input, c.n, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("truncate(%q, %d) produced invalid UTF-8: %q", c.input, c.n, got)
		}
	}
}

func TestTypeEmoji(t *testing.T) {
	if typeEmoji("question") != "❓" {
		t.Error("expected ❓ for question")
	}
	if typeEmoji("work") != "🔧" {
		t.Error("expected 🔧 for work")
	}
	if typeEmoji("other") != "🔧" {
		t.Error("expected 🔧 for unknown type")
	}
}

func TestFormatAdded(t *testing.T) {
	cases := []struct {
		itemType string
		id       int64
		user     string
		wantSub  string
	}{
		{"question", 5, "U001", "Question *#5*"},
		{"work", 7, "U002", "Work item *#7*"},
	}
	for _, c := range cases {
		got := FormatAdded(c.itemType, c.id, c.user)
		if got == "" {
			t.Errorf("FormatAdded(%q) returned empty", c.itemType)
		}
		if !strings.Contains(got, c.wantSub) {
			t.Errorf("FormatAdded(%q) = %q, want substring %q", c.itemType, got, c.wantSub)
		}
		// Must NOT echo the question/work content back.
		if strings.Contains(got, "creds") || strings.Contains(got, "runbook") {
			t.Errorf("FormatAdded should not echo content, got: %q", got)
		}
	}
}

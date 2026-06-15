package bot

import (
	"os"
	"testing"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	f, err := os.CreateTemp("", "queue-bot-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	db, err := NewDB(f.Name())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestAddAndList(t *testing.T) {
	db := newTestDB(t)

	id1, err := db.AddItem("C001", "question", "How to rotate creds?", "U001", "ts.001")
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	id2, err := db.AddItem("C001", "work", "Update runbook", "U002", "ts.002")
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	// Different channel — must not appear.
	db.AddItem("C999", "work", "Other channel", "U001", "ts.003")

	items, err := db.ListOpen("C001")
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != id1 || items[1].ID != id2 {
		t.Errorf("unexpected IDs: %d %d", items[0].ID, items[1].ID)
	}
}

func TestAssignItem(t *testing.T) {
	db := newTestDB(t)
	id, _ := db.AddItem("C001", "question", "Who owns this?", "U001", "ts.1")

	if err := db.AssignItem(id, "C001", "U002"); err != nil {
		t.Fatalf("AssignItem: %v", err)
	}
	item, _ := db.GetItem(id, "C001")
	if item.AssigneeID == nil || *item.AssigneeID != "U002" {
		t.Errorf("expected assignee U002, got %v", item.AssigneeID)
	}
}

func TestAssignItemNotFound(t *testing.T) {
	db := newTestDB(t)
	if err := db.AssignItem(999, "C001", "U002"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAssignItemCrossChannel(t *testing.T) {
	db := newTestDB(t)
	id, _ := db.AddItem("C001", "question", "Q", "U001", "ts.1")
	if err := db.AssignItem(id, "C999", "U002"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound for cross-channel assign, got %v", err)
	}
}

func TestDoneItem(t *testing.T) {
	db := newTestDB(t)
	id, _ := db.AddItem("C001", "work", "Finish the runbook", "U001", "ts.1")

	item, err := db.DoneItem(id, "C001")
	if err != nil {
		t.Fatalf("DoneItem: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected status done, got %s", item.Status)
	}
	items, _ := db.ListOpen("C001")
	if len(items) != 0 {
		t.Errorf("expected 0 open items, got %d", len(items))
	}
}

func TestDoneItemAlreadyDone(t *testing.T) {
	db := newTestDB(t)
	id, _ := db.AddItem("C001", "work", "Task", "U001", "ts.1")
	db.DoneItem(id, "C001")
	if _, err := db.DoneItem(id, "C001"); err != ErrAlreadyDone {
		t.Errorf("expected ErrAlreadyDone, got %v", err)
	}
}

func TestAssignAlreadyDone(t *testing.T) {
	db := newTestDB(t)
	id, _ := db.AddItem("C001", "work", "Task", "U001", "ts.1")
	db.DoneItem(id, "C001")
	if err := db.AssignItem(id, "C001", "U002"); err != ErrAlreadyDone {
		t.Errorf("expected ErrAlreadyDone, got %v", err)
	}
}

func TestDoneItemByMsgTS(t *testing.T) {
	db := newTestDB(t)
	db.AddItem("C001", "question", "Thread question", "U001", "1700000000.111")

	item, err := db.DoneItemByMsgTS("C001", "1700000000.111")
	if err != nil {
		t.Fatalf("DoneItemByMsgTS: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected done, got %s", item.Status)
	}
	// Second resolve must return ErrAlreadyDone.
	if _, err := db.DoneItemByMsgTS("C001", "1700000000.111"); err != ErrAlreadyDone {
		t.Errorf("expected ErrAlreadyDone on second done, got %v", err)
	}
}

func TestDoneItemByMsgTSNotFound(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.DoneItemByMsgTS("C001", "no.such.ts"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListOpenEmpty(t *testing.T) {
	db := newTestDB(t)
	items, err := db.ListOpen("C001")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

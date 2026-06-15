package bot

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	ErrNotFound    = errors.New("item not found")
	ErrAlreadyDone = errors.New("item already resolved")
)

type DB struct{ conn *sql.DB }

// NewDB opens (or creates) the SQLite database at path and runs migrations.
func NewDB(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	conn.SetMaxOpenConns(1) // SQLite: single writer
	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func (d *DB) Close() error { return d.conn.Close() }

func (d *DB) migrate() error {
	_, err := d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS items (
			id           INTEGER  PRIMARY KEY AUTOINCREMENT,
			channel_id   TEXT     NOT NULL,
			type         TEXT     NOT NULL DEFAULT 'question',
			content      TEXT     NOT NULL,
			submitter_id TEXT     NOT NULL,
			assignee_id  TEXT,
			status       TEXT     NOT NULL DEFAULT 'open',
			msg_ts       TEXT,
			created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at   DATETIME NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_items_channel_status ON items(channel_id, status);
		CREATE INDEX IF NOT EXISTS idx_items_msg_ts         ON items(channel_id, msg_ts);
	`)
	return err
}

func (d *DB) AddItem(channelID, itemType, content, submitterID, msgTS string) (int64, error) {
	res, err := d.conn.Exec(
		`INSERT INTO items (channel_id, type, content, submitter_id, msg_ts) VALUES (?, ?, ?, ?, ?)`,
		channelID, itemType, content, submitterID, msgTS,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListOpen(channelID string) ([]Item, error) {
	rows, err := d.conn.Query(
		`SELECT id, channel_id, type, content, submitter_id, assignee_id, status, msg_ts, created_at, updated_at
		   FROM items WHERE channel_id = ? AND status = 'open' ORDER BY id ASC`,
		channelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *it)
	}
	return items, rows.Err()
}

func (d *DB) GetItem(id int64, channelID string) (*Item, error) {
	row := d.conn.QueryRow(
		`SELECT id, channel_id, type, content, submitter_id, assignee_id, status, msg_ts, created_at, updated_at
		   FROM items WHERE id = ? AND channel_id = ?`,
		id, channelID,
	)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return it, err
}

// GetItemByMsgTS looks up an item by the Slack message timestamp of the original command.
// This enables bare `!wdone` posted as a thread reply to resolve the parent item.
func (d *DB) GetItemByMsgTS(channelID, msgTS string) (*Item, error) {
	row := d.conn.QueryRow(
		`SELECT id, channel_id, type, content, submitter_id, assignee_id, status, msg_ts, created_at, updated_at
		   FROM items WHERE channel_id = ? AND msg_ts = ?`,
		channelID, msgTS,
	)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return it, err
}

func (d *DB) AssignItem(id int64, channelID, assigneeID string) error {
	item, err := d.GetItem(id, channelID)
	if err != nil {
		return err
	}
	if item.Status == "done" {
		return ErrAlreadyDone
	}
	_, err = d.conn.Exec(
		`UPDATE items SET assignee_id = ?, updated_at = datetime('now') WHERE id = ? AND channel_id = ?`,
		assigneeID, id, channelID,
	)
	return err
}

func (d *DB) DoneItem(id int64, channelID string) (*Item, error) {
	item, err := d.GetItem(id, channelID)
	if err != nil {
		return nil, err
	}
	if item.Status == "done" {
		return nil, ErrAlreadyDone
	}
	_, err = d.conn.Exec(
		`UPDATE items SET status = 'done', updated_at = datetime('now') WHERE id = ? AND channel_id = ?`,
		id, channelID,
	)
	if err != nil {
		return nil, err
	}
	item.Status = "done"
	return item, nil
}

// DoneItemByMsgTS resolves the item whose original command message has the given Slack timestamp.
func (d *DB) DoneItemByMsgTS(channelID, msgTS string) (*Item, error) {
	item, err := d.GetItemByMsgTS(channelID, msgTS)
	if err != nil {
		return nil, err
	}
	return d.DoneItem(item.ID, channelID)
}

// scanner abstracts *sql.Row and *sql.Rows for scanItem.
type scanner interface{ Scan(dest ...any) error }

func scanItem(s scanner) (*Item, error) {
	var it Item
	var assigneeID, msgTS sql.NullString
	var createdAt, updatedAt string
	err := s.Scan(&it.ID, &it.ChannelID, &it.Type, &it.Content,
		&it.SubmitterID, &assigneeID, &it.Status, &msgTS, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if assigneeID.Valid {
		it.AssigneeID = &assigneeID.String
	}
	if msgTS.Valid {
		it.MsgTS = msgTS.String
	}
	it.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	it.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	return &it, nil
}

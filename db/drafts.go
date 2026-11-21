package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mrmohebi/divar-alert/platform"
)

// ErrStaleDraft means the draft changed or was confirmed in the meantime.
var ErrStaleDraft = errors.New("draft has already changed or been confirmed")

// Draft returns the chat's JSON-encoded unfinished alert, or "" if none.
func (db *DB) Draft(chat platform.Chat) (string, error) {
	var data string
	err := db.conn.QueryRow(`SELECT data FROM drafts WHERE platform = ? AND chat_id = ?`, chat.Platform, chat.ID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return data, err
}

// SaveDraft stores the chat's unfinished alert.
func (db *DB) SaveDraft(chat platform.Chat, data string) error {
	_, err := db.conn.Exec(`INSERT INTO drafts (platform, chat_id, data, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(platform, chat_id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
		chat.Platform, chat.ID, data, time.Now().Unix())
	return err
}

// DeleteDraft removes the chat's unfinished alert, if any.
func (db *DB) DeleteDraft(chat platform.Chat) error {
	_, err := db.conn.Exec(`DELETE FROM drafts WHERE platform = ? AND chat_id = ?`, chat.Platform, chat.ID)
	return err
}

// ConfirmDraft atomically consumes the exact draft the user confirmed and
// creates its alert, so a repeated confirmation cannot create a second alert.
func (db *DB) ConfirmDraft(chat platform.Chat, expected string, alert Alert) error {
	if alert.Chat != chat {
		return fmt.Errorf("alert does not belong to the draft's chat")
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM drafts WHERE platform = ? AND chat_id = ? AND data = ?`, chat.Platform, chat.ID, expected)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrStaleDraft
	}
	if err := insertAlert(tx, alert); err != nil {
		return err
	}
	return tx.Commit()
}

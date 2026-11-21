package db

import (
	"encoding/json"
	"fmt"
	"time"
)

// Delivered reports whether the listing was already sent for the alert's
// current generation.
func (db *DB) Delivered(alertID int64, token string) (bool, error) {
	var exists bool
	err := db.conn.QueryRow(`SELECT EXISTS(SELECT 1 FROM deliveries WHERE alert_id = ? AND token = ? AND retired_at IS NULL)`, alertID, token).Scan(&exists)
	return exists, err
}

// RecordDelivery stores a successful notification so it is not sent again.
func (db *DB) RecordDelivery(alertID int64, token string, data any, at int64) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec(`INSERT INTO deliveries (alert_id, token, data, created_at) VALUES (?, ?, ?, ?)`, alertID, token, string(raw), at)
	return err
}

// LoadHistory returns the alert's latest listings, oldest first, for AI
// comparison.
func (db *DB) LoadHistory(alertID int64) ([]any, error) {
	rows, err := db.conn.Query(`SELECT data FROM (
		SELECT id, data FROM alert_history WHERE alert_id = ? ORDER BY id DESC LIMIT ?
	) ORDER BY id`, alertID, maxHistoryPosts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var history []any
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var post any
		if err := json.Unmarshal([]byte(raw), &post); err != nil {
			return nil, fmt.Errorf("decode alert %d history: %w", alertID, err)
		}
		history = append(history, post)
	}
	return history, rows.Err()
}

// AppendHistory stores a listing for AI comparison and keeps only the
// alert's latest entries.
func (db *DB) AppendHistory(alertID int64, post any) error {
	raw, err := json.Marshal(post)
	if err != nil {
		return err
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO alert_history (alert_id, data, created_at) VALUES (?, ?, ?)`, alertID, string(raw), time.Now().Unix()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM alert_history WHERE alert_id = ? AND id NOT IN (
		SELECT id FROM alert_history WHERE alert_id = ? ORDER BY id DESC LIMIT ?
	)`, alertID, alertID, maxHistoryPosts); err != nil {
		return err
	}
	return tx.Commit()
}

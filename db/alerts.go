package db

import (
	"database/sql"
	"sync"
	"time"

	"github.com/mrmohebi/divar-alert/platform"
)

// Alert is a saved Divar search that notifies one chat about new listings.
type Alert struct {
	ID    int64
	Chat  platform.Chat
	Title string
	// SearchQuery is the JSON-encoded divar.SearchQuery to watch.
	SearchQuery string
	// OriginalPrompt is the owner's own description of what they want.
	OriginalPrompt string
	AIEnabled      bool
	// AnalysisPrompt holds extra preferences for AI commentary, one per line.
	AnalysisPrompt string
	Interval       int
	LastChecked    int64
	// WatchState is the JSON-encoded scan checkpoint.
	WatchState string
	// Generation increases on every reset; work from an older generation is
	// discarded.
	Generation int64
	DeletedAt  *int64
}

const alertColumns = `id, platform, chat_id, title, search_query, original_prompt, ai_enabled, analysis_prompt, interval_seconds, last_checked, watch_state, generation, deleted_at`

func scanAlerts(rows *sql.Rows) ([]Alert, error) {
	defer rows.Close()
	var alerts []Alert
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.ID, &a.Chat.Platform, &a.Chat.ID, &a.Title, &a.SearchQuery, &a.OriginalPrompt, &a.AIEnabled, &a.AnalysisPrompt, &a.Interval, &a.LastChecked, &a.WatchState, &a.Generation, &a.DeletedAt); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

// execer is satisfied by both *sql.DB and *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func insertAlert(e execer, a Alert) error {
	if a.WatchState == "" {
		a.WatchState = "{}"
	}
	_, err := e.Exec(`INSERT INTO alerts (id, platform, chat_id, title, search_query, original_prompt, ai_enabled, analysis_prompt, interval_seconds, last_checked, watch_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Chat.Platform, a.Chat.ID, a.Title, a.SearchQuery, a.OriginalPrompt, a.AIEnabled, a.AnalysisPrompt, a.Interval, a.LastChecked, a.WatchState)
	return err
}

// InsertAlert saves a new alert.
func (db *DB) InsertAlert(a Alert) error {
	return insertAlert(db.conn, a)
}

// ActiveAlerts returns every alert that has not been deleted.
func (db *DB) ActiveAlerts() ([]Alert, error) {
	rows, err := db.conn.Query(`SELECT ` + alertColumns + ` FROM alerts WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return scanAlerts(rows)
}

// ChatAlerts returns the active alerts of one chat.
func (db *DB) ChatAlerts(chat platform.Chat) ([]Alert, error) {
	rows, err := db.conn.Query(`SELECT `+alertColumns+` FROM alerts WHERE platform = ? AND chat_id = ? AND deleted_at IS NULL ORDER BY id`, chat.Platform, chat.ID)
	if err != nil {
		return nil, err
	}
	return scanAlerts(rows)
}

// SetAlertAI turns AI commentary on or off for one of the chat's alerts.
func (db *DB) SetAlertAI(chat platform.Chat, alertID int64, enabled bool) error {
	return requireAffected(db.conn.Exec(`UPDATE alerts SET ai_enabled = ? WHERE id = ? AND platform = ? AND chat_id = ? AND deleted_at IS NULL`,
		enabled, alertID, chat.Platform, chat.ID))
}

// DeleteAlert soft-deletes one of the chat's alerts. It waits for an
// in-flight delivery, and no delivery for the alert starts afterwards.
func (db *DB) DeleteAlert(chat platform.Chat, alertID int64) error {
	lock := db.alertLock(alertID)
	lock.Lock()
	defer lock.Unlock()
	return requireAffected(db.conn.Exec(`UPDATE alerts SET deleted_at = ? WHERE id = ? AND platform = ? AND chat_id = ? AND deleted_at IS NULL`,
		time.Now().Unix(), alertID, chat.Platform, chat.ID))
}

// ResetAlert starts one of the chat's alerts over: it retires deliveries,
// clears AI history and the scan checkpoint, and starts a new generation so
// work begun before the reset is discarded.
func (db *DB) ResetAlert(chat platform.Chat, alertID int64) error {
	lock := db.alertLock(alertID)
	lock.Lock()
	defer lock.Unlock()
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireAffected(tx.Exec(`UPDATE alerts SET last_checked = 0, watch_state = '{}', generation = generation + 1
		WHERE id = ? AND platform = ? AND chat_id = ? AND deleted_at IS NULL`, alertID, chat.Platform, chat.ID)); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE deliveries SET retired_at = ? WHERE alert_id = ? AND retired_at IS NULL`, time.Now().Unix(), alertID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM alert_history WHERE alert_id = ?`, alertID); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) alertLock(id int64) *sync.Mutex {
	value, _ := db.alertLocks.LoadOrStore(id, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// WithActiveAlert runs fn only while this snapshot of the alert is still
// current: not deleted, not reset, and owned by the same chat. Once DeleteAlert
// or ResetAlert returns, no worker holding an older snapshot can deliver. fn
// may use other DB methods but must not delete, reset, or nest WithActiveAlert
// for the same alert.
func (db *DB) WithActiveAlert(a Alert, fn func() error) error {
	lock := db.alertLock(a.ID)
	lock.Lock()
	defer lock.Unlock()
	var active bool
	err := db.conn.QueryRow(`SELECT EXISTS(SELECT 1 FROM alerts WHERE id = ? AND platform = ? AND chat_id = ? AND generation = ? AND deleted_at IS NULL)`,
		a.ID, a.Chat.Platform, a.Chat.ID, a.Generation).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return sql.ErrNoRows
	}
	return fn()
}

// SaveWatchState stores a scan checkpoint, but only for the generation that
// produced it, so a slow scan cannot undo a reset or deletion.
func (db *DB) SaveWatchState(alertID, generation int64, state string, checked int64) error {
	return requireAffected(db.conn.Exec(`UPDATE alerts SET watch_state = ?, last_checked = ? WHERE id = ? AND generation = ? AND deleted_at IS NULL`,
		state, checked, alertID, generation))
}

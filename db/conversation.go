package db

import (
	"errors"
	"fmt"
)

var ErrStaleDraft = errors.New("draft has already changed or been confirmed")

const insertAlertSQL = `INSERT INTO alerts (id,chat_id,platform,title,link,ai_enabled,analysis_prompt,interval_seconds,last_time_checked,search_query,original_prompt,watch_state) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`

// Additive migration preserves existing SQLite alerts and supports reopening.
func (db *DB) migrateAlerts() error {
	rows, err := db.conn.Query(`PRAGMA table_info(alerts)`)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var def any
		if err = rows.Scan(&cid, &name, &kind, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		names[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{
		{"search_query", `TEXT NOT NULL DEFAULT '' CHECK(search_query = '' OR json_valid(search_query))`},
		{"original_prompt", `TEXT NOT NULL DEFAULT ''`},
		{"deleted_at", `INTEGER`},
		{"generation", `INTEGER NOT NULL DEFAULT 0`},
		{"watch_state", `TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(watch_state))`},
		{"platform", `TEXT NOT NULL DEFAULT ''`},
	} {
		if !names[column.name] {
			if _, err = db.conn.Exec("ALTER TABLE alerts ADD COLUMN " + column.name + " " + column.definition); err != nil {
				return fmt.Errorf("migrate %s: %w", column.name, err)
			}
		}
	}
	return nil
}

// ConfirmDraft atomically consumes the exact saved draft and creates one alert.
// A second callback cannot create a second alert, including after a restart.
func (db *DB) ConfirmDraft(chatID int64, key, expected string, alert Alert) error {
	if alert.Platform == "" {
		alert.Platform = db.platform
	}
	if alert.ChatId != chatID || alert.Platform != db.platform {
		return fmt.Errorf("alert does not belong to draft owner")
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM processes WHERE platform=? AND chat_id=? AND process_key=? AND process_data=?`, db.platform, chatID, key, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrStaleDraft
	}
	if alert.WatchState == "" {
		alert.WatchState = "{}"
	}
	_, err = tx.Exec(insertAlertSQL, alert.Id, alert.ChatId, alert.Platform, alert.Title, alert.Link, alert.AIEnabled, alert.AnalysisPrompt, alert.Interval, alert.LastTimeChecked, alert.SearchQuery, alert.OriginalPrompt, alert.WatchState)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) UpdateWatchState(alertID int64, state string, lastChecked int64) error {
	return requireAffected(db.conn.Exec(`UPDATE alerts SET watch_state=?,last_time_checked=? WHERE id=? AND deleted_at IS NULL`, state, lastChecked, alertID))
}

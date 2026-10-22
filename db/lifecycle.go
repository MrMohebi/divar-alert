package db

import (
	"database/sql"
	"sync"
)

// Migrate delivery records without losing prior successful notifications. A reset
// retires the deduplication key but retains each delivery as a separate audit row.
func (db *DB) migrateDeliveries() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`PRAGMA table_info(seen_posts)`)
	if err != nil {
		return err
	}
	hasID := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var def any
		if err := rows.Scan(&cid, &name, &kind, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "id" {
			hasID = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !hasID {
		_, err = tx.Exec(`CREATE TABLE seen_posts_retained (
   id INTEGER PRIMARY KEY AUTOINCREMENT,
   token TEXT NOT NULL, alert_id INTEGER NOT NULL,
   data TEXT CHECK(data IS NULL OR json_valid(data)),
   created_at INTEGER NOT NULL, deleted_at INTEGER,
   FOREIGN KEY(alert_id) REFERENCES alerts(id)
  );
  INSERT INTO seen_posts_retained(token,alert_id,data,created_at)
   SELECT token,alert_id,data,created_at FROM seen_posts ORDER BY rowid;
  DROP TABLE seen_posts;
  ALTER TABLE seen_posts_retained RENAME TO seen_posts;`)
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_active_delivery ON seen_posts(alert_id,token) WHERE deleted_at IS NULL;
 CREATE INDEX IF NOT EXISTS idx_delivery_alert_time ON seen_posts(alert_id,created_at);`); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) alertLock(id int64) *sync.Mutex {
	value, _ := db.lifecycle.LoadOrStore(id, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// WithActiveAlert serializes delivery with delete/reset. Once either mutation
// returns, workers holding an older alert snapshot cannot send or record posts.
// The callback may use database methods, but must not call delete/reset or nest
// WithActiveAlert for the same alert.
func (db *DB) WithActiveAlert(alert Alert, fn func() error) error {
	lock := db.alertLock(alert.Id)
	lock.Lock()
	defer lock.Unlock()
	var active bool
	err := db.conn.QueryRow(`SELECT EXISTS(SELECT 1 FROM alerts WHERE id=? AND chat_id=? AND generation=? AND deleted_at IS NULL)`, alert.Id, alert.ChatId, alert.Generation).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return sql.ErrNoRows
	}
	return fn()
}

// Save only the scan generation that produced this checkpoint. A reset or
// deletion cannot be undone by a delayed search response or deferred save.
func (db *DB) UpdateWatchStateForGeneration(alertID, generation int64, state string, lastChecked int64) error {
	return requireAffected(db.conn.Exec(`UPDATE alerts SET watch_state=?,last_time_checked=? WHERE id=? AND generation=? AND deleted_at IS NULL`, state, lastChecked, alertID, generation))
}

func (db *DB) UpdateAlertLastCheckedForGeneration(alertID, generation, lastChecked int64) error {
	return requireAffected(db.conn.Exec(`UPDATE alerts SET last_time_checked=? WHERE id=? AND generation=? AND deleted_at IS NULL`, lastChecked, alertID, generation))
}

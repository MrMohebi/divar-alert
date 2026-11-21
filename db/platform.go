package db

import "fmt"

// migrateProcesses rebuilds a pre-platform processes table, whose primary key
// was chat_id alone, so one chat ID can hold a draft on each platform.
func (db *DB) migrateProcesses() error {
	rows, err := db.conn.Query(`PRAGMA table_info(processes)`)
	if err != nil {
		return err
	}
	hasPlatform := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var def any
		if err := rows.Scan(&cid, &name, &kind, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "platform" {
			hasPlatform = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || hasPlatform {
		return err
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE processes_scoped (
		platform TEXT NOT NULL DEFAULT '',
		chat_id INTEGER NOT NULL,
		process_key TEXT NOT NULL,
		process_data TEXT NOT NULL CHECK(json_valid(process_data)),
		last_action_at INTEGER NOT NULL,
		PRIMARY KEY (platform, chat_id)
	);
	INSERT INTO processes_scoped (platform, chat_id, process_key, process_data, last_action_at)
		SELECT '', chat_id, process_key, process_data, last_action_at FROM processes;
	DROP TABLE processes;
	ALTER TABLE processes_scoped RENAME TO processes;`); err != nil {
		return fmt.Errorf("migrate processes: %w", err)
	}
	return tx.Commit()
}

// CountUnscoped reports alerts and drafts created before platform support.
func (db *DB) CountUnscoped() (int, error) {
	var count int
	err := db.conn.QueryRow(`SELECT (SELECT COUNT(*) FROM alerts WHERE platform = '') + (SELECT COUNT(*) FROM processes WHERE platform = '')`).Scan(&count)
	return count, err
}

// AdoptUnscoped assigns alerts and drafts created before platform support to
// the platform whose bot created them. It returns how many rows it changed.
func (db *DB) AdoptUnscoped(platform string) (int64, error) {
	if platform == "" {
		return 0, fmt.Errorf("platform is required")
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var total int64
	for _, statement := range []string{
		`UPDATE alerts SET platform = ? WHERE platform = ''`,
		`UPDATE processes SET platform = ? WHERE platform = ''`,
	} {
		result, err := tx.Exec(statement, platform)
		if err != nil {
			return 0, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, tx.Commit()
}

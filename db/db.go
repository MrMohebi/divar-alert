package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/duckdb/duckdb-go/v2"
)

// DB wraps the DuckDB connection
type DB struct {
	conn *sql.DB
}

// Open opens a DuckDB database and creates tables if they don't exist
func Open(path string) (*DB, error) {
	// Use Connector for better connection control
	connector, err := duckdb.NewConnector(path, func(execer driver.ExecerContext) error {
		// Initialize connection settings
		_, err := execer.ExecContext(context.Background(), "SET threads TO 1", nil)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create duckdb connector: %w", err)
	}

	conn := sql.OpenDB(connector)

	// Use single connection to avoid visibility issues
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	db := &DB{conn: conn}
	if err := db.createTables(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return db, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.conn.Close()
}

// createTables creates the database schema
func (db *DB) createTables() error {
	schema := `
		CREATE TABLE IF NOT EXISTS alerts (
			id BIGINT PRIMARY KEY,
			chat_id BIGINT NOT NULL,
			title TEXT NOT NULL,
			link TEXT NOT NULL,
			interval_seconds INTEGER NOT NULL,
			last_time_checked BIGINT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_alerts_chat_id ON alerts(chat_id);

		CREATE TABLE IF NOT EXISTS seen_posts (
			token TEXT NOT NULL,
			alert_id BIGINT NOT NULL,
			data JSON,
			created_at BIGINT NOT NULL,
			PRIMARY KEY (token, alert_id)
		);

		CREATE TABLE IF NOT EXISTS processes (
			chat_id BIGINT PRIMARY KEY,
			process_key TEXT NOT NULL,
			process_data JSON NOT NULL,
			last_action_at BIGINT NOT NULL
		);
	`
	_, err := db.conn.Exec(schema)
	return err
}

// Alert represents an alert configuration
type Alert struct {
	Id              int64  `json:"id"`
	Title           string `json:"title"`
	Link            string `json:"link"`
	Interval        int    `json:"interval"`
	ChatId          int64  `json:"chatId"`
	LastTimeChecked int64  `json:"lastTimeChecked"`
}

// GetAllAlerts returns all alerts from the database
func (db *DB) GetAllAlerts() ([]Alert, error) {
	rows, err := db.conn.Query(`
		SELECT id, chat_id, title, link, interval_seconds, last_time_checked
		FROM alerts
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []Alert
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.Id, &a.ChatId, &a.Title, &a.Link, &a.Interval, &a.LastTimeChecked); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

// GetAlertsByChatId returns all alerts for a specific chat
func (db *DB) GetAlertsByChatId(chatId int64) ([]Alert, error) {
	rows, err := db.conn.Query(`
		SELECT id, chat_id, title, link, interval_seconds, last_time_checked
		FROM alerts
		WHERE chat_id = ?
	`, chatId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []Alert
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.Id, &a.ChatId, &a.Title, &a.Link, &a.Interval, &a.LastTimeChecked); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

// InsertAlert inserts a new alert
func (db *DB) InsertAlert(a Alert) error {
	_, err := db.conn.Exec(`
		INSERT INTO alerts (id, chat_id, title, link, interval_seconds, last_time_checked)
		VALUES (?, ?, ?, ?, ?, ?)
	`, a.Id, a.ChatId, a.Title, a.Link, a.Interval, a.LastTimeChecked)
	return err
}

// DeleteAlert deletes an alert by id and chat_id
func (db *DB) DeleteAlert(chatId, alertId int64) error {
	_, err := db.conn.Exec(`DELETE FROM alerts WHERE chat_id = ? AND id = ?`, chatId, alertId)
	return err
}

// UpdateAlertLastChecked updates the last_time_checked for an alert
func (db *DB) UpdateAlertLastChecked(alertId, lastTimeChecked int64) error {
	_, err := db.conn.Exec(`UPDATE alerts SET last_time_checked = ? WHERE id = ?`, lastTimeChecked, alertId)
	return err
}

// PostExists checks if a post has been seen for an alert
func (db *DB) PostExists(token string, alertId int64) (bool, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM seen_posts WHERE token = ? AND alert_id = ?`, token, alertId).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// InsertPost inserts a seen post record
func (db *DB) InsertPost(token string, alertId int64, data interface{}, createdAt int64) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec(`
		INSERT INTO seen_posts (token, alert_id, data, created_at)
		VALUES (?, ?, ?, ?)
	`, token, alertId, string(jsonData), createdAt)
	return err
}

// Process represents a multi-step wizard state
type Process struct {
	ChatId       int64  `json:"chatId"`
	ProcessKey   string `json:"processKey"`
	ProcessData  string `json:"processData"`
	LastActionAt int64  `json:"lastActionAt"`
}

// GetProcess retrieves the current process for a chat
func (db *DB) GetProcess(chatId int64) (*Process, error) {
	var p Process
	err := db.conn.QueryRow(`
		SELECT chat_id, process_key, CAST(process_data AS VARCHAR) as process_data, last_action_at
		FROM processes
		WHERE chat_id = ?
	`, chatId).Scan(&p.ChatId, &p.ProcessKey, &p.ProcessData, &p.LastActionAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertProcess inserts or updates a process
func (db *DB) UpsertProcess(chatId int64, processKey string, processData string, lastActionAt int64) error {
	// Use DELETE + INSERT instead of ON CONFLICT for better compatibility
	_, err := db.conn.Exec(`DELETE FROM processes WHERE chat_id = ?`, chatId)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec(`
		INSERT INTO processes (chat_id, process_key, process_data, last_action_at)
		VALUES (?, ?, ?, ?)
	`, chatId, processKey, processData, lastActionAt)
	return err
}

// DeleteProcess deletes a process for a chat
func (db *DB) DeleteProcess(chatId int64) error {
	_, err := db.conn.Exec(`DELETE FROM processes WHERE chat_id = ?`, chatId)
	return err
}

// GetAllProcesses retrieves all processes from the database
func (db *DB) GetAllProcesses() ([]Process, error) {
	rows, err := db.conn.Query(`
		SELECT chat_id, process_key, CAST(process_data AS VARCHAR) as process_data, last_action_at
		FROM processes
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var processes []Process
	for rows.Next() {
		var p Process
		if err := rows.Scan(&p.ChatId, &p.ProcessKey, &p.ProcessData, &p.LastActionAt); err != nil {
			return nil, err
		}
		processes = append(processes, p)
	}
	return processes, rows.Err()
}

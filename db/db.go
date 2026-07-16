package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite connection.
type DB struct {
	conn *sql.DB
}

// Open opens a SQLite database and creates its schema when needed.
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("configure sqlite database (use a new DB_PATH, not the old DuckDB file): %w", err)
		}
	}

	database := &DB{conn: conn}
	if err := database.createTables(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("create sqlite schema (use a new DB_PATH, not the old DuckDB file): %w", err)
	}
	return database, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) createTables() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS alerts (
			id INTEGER PRIMARY KEY,
			chat_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			link TEXT NOT NULL,
			ai_enabled BOOLEAN NOT NULL DEFAULT true,
			analysis_prompt TEXT NOT NULL DEFAULT '',
			interval_seconds INTEGER NOT NULL CHECK(interval_seconds BETWEEN 10 AND 86400),
			last_time_checked INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_alerts_chat_id ON alerts(chat_id);

		CREATE TABLE IF NOT EXISTS seen_posts (
			token TEXT NOT NULL,
			alert_id INTEGER NOT NULL,
			data TEXT CHECK(data IS NULL OR json_valid(data)),
			created_at INTEGER NOT NULL,
			PRIMARY KEY (token, alert_id),
			FOREIGN KEY (alert_id) REFERENCES alerts(id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS alert_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			alert_id INTEGER NOT NULL,
			data TEXT NOT NULL CHECK(json_valid(data)),
			created_at INTEGER NOT NULL,
			FOREIGN KEY (alert_id) REFERENCES alerts(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_alert_history_alert_id ON alert_history(alert_id, id);

		CREATE TABLE IF NOT EXISTS processes (
			chat_id INTEGER PRIMARY KEY,
			process_key TEXT NOT NULL,
			process_data TEXT NOT NULL CHECK(json_valid(process_data)),
			last_action_at INTEGER NOT NULL
		);
	`)
	return err
}

// Alert represents an alert configuration.
type Alert struct {
	Id              int64  `json:"id"`
	Title           string `json:"title"`
	Link            string `json:"link"`
	AIEnabled       bool   `json:"aiEnabled"`
	AnalysisPrompt  string `json:"analysisPrompt"`
	Interval        int    `json:"interval"`
	ChatId          int64  `json:"chatId"`
	LastTimeChecked int64  `json:"lastTimeChecked"`
}

const selectAlerts = `
	SELECT id, chat_id, title, link, ai_enabled, analysis_prompt, interval_seconds, last_time_checked
	FROM alerts`

func scanAlerts(rows *sql.Rows) ([]Alert, error) {
	var alerts []Alert
	for rows.Next() {
		var alert Alert
		if err := rows.Scan(&alert.Id, &alert.ChatId, &alert.Title, &alert.Link, &alert.AIEnabled, &alert.AnalysisPrompt, &alert.Interval, &alert.LastTimeChecked); err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	return alerts, rows.Err()
}

func (db *DB) GetAllAlerts() ([]Alert, error) {
	rows, err := db.conn.Query(selectAlerts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlerts(rows)
}

func (db *DB) GetAlertsByChatId(chatId int64) ([]Alert, error) {
	rows, err := db.conn.Query(selectAlerts+` WHERE chat_id = ?`, chatId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlerts(rows)
}

func (db *DB) InsertAlert(alert Alert) error {
	_, err := db.conn.Exec(`
		INSERT INTO alerts (id, chat_id, title, link, ai_enabled, analysis_prompt, interval_seconds, last_time_checked)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, alert.Id, alert.ChatId, alert.Title, alert.Link, alert.AIEnabled, alert.AnalysisPrompt, alert.Interval, alert.LastTimeChecked)
	return err
}

func (db *DB) UpdateAlertAIEnabled(chatId, alertId int64, enabled bool) error {
	return requireAffected(db.conn.Exec(`UPDATE alerts SET ai_enabled = ? WHERE chat_id = ? AND id = ?`, enabled, chatId, alertId))
}

func (db *DB) UpdateAlertAnalysis(chatId, alertId int64, prompt string) error {
	return requireAffected(db.conn.Exec(`UPDATE alerts SET ai_enabled = true, analysis_prompt = ? WHERE chat_id = ? AND id = ?`, prompt, chatId, alertId))
}

func (db *DB) DeleteAlert(chatId, alertId int64) error {
	return requireAffected(db.conn.Exec(`DELETE FROM alerts WHERE chat_id = ? AND id = ?`, chatId, alertId))
}

func requireAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (db *DB) UpdateAlertLastChecked(alertId, lastTimeChecked int64) error {
	_, err := db.conn.Exec(`UPDATE alerts SET last_time_checked = ? WHERE id = ?`, lastTimeChecked, alertId)
	return err
}

func (db *DB) PostExists(token string, alertId int64) (bool, error) {
	var exists bool
	err := db.conn.QueryRow(`SELECT EXISTS(SELECT 1 FROM seen_posts WHERE token = ? AND alert_id = ?)`, token, alertId).Scan(&exists)
	return exists, err
}

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

// LoadHistory restores prior posts in insertion order for AI context.
func (db *DB) LoadHistory(alertId int64) ([]interface{}, error) {
	rows, err := db.conn.Query(`SELECT data FROM alert_history WHERE alert_id = ? ORDER BY id`, alertId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []interface{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var post interface{}
		if err := json.Unmarshal([]byte(raw), &post); err != nil {
			return nil, fmt.Errorf("decode alert %d history: %w", alertId, err)
		}
		history = append(history, post)
	}
	return history, rows.Err()
}

// AppendHistory stores a post for future AI context.
func (db *DB) AppendHistory(alertId int64, post interface{}) error {
	data, err := json.Marshal(post)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec(`INSERT INTO alert_history (alert_id, data, created_at) VALUES (?, ?, ?)`, alertId, string(data), time.Now().Unix())
	return err
}

// ResetAlert clears all prior posts and makes an owned alert immediately due.
func (db *DB) ResetAlert(chatId, alertId int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := requireAffected(tx.Exec(`UPDATE alerts SET last_time_checked = 0 WHERE chat_id = ? AND id = ?`, chatId, alertId)); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM seen_posts WHERE alert_id = ?`, alertId); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM alert_history WHERE alert_id = ?`, alertId); err != nil {
		return err
	}
	return tx.Commit()
}

// Process represents a multi-step wizard state.
type Process struct {
	ChatId       int64  `json:"chatId"`
	ProcessKey   string `json:"processKey"`
	ProcessData  string `json:"processData"`
	LastActionAt int64  `json:"lastActionAt"`
}

func (db *DB) GetProcess(chatId int64) (*Process, error) {
	var process Process
	err := db.conn.QueryRow(`
		SELECT chat_id, process_key, process_data, last_action_at
		FROM processes
		WHERE chat_id = ?
	`, chatId).Scan(&process.ChatId, &process.ProcessKey, &process.ProcessData, &process.LastActionAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &process, nil
}

func (db *DB) UpsertProcess(chatId int64, processKey string, processData string, lastActionAt int64) error {
	_, err := db.conn.Exec(`
		INSERT INTO processes (chat_id, process_key, process_data, last_action_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(chat_id) DO UPDATE SET
			process_key = excluded.process_key,
			process_data = excluded.process_data,
			last_action_at = excluded.last_action_at
	`, chatId, processKey, processData, lastActionAt)
	return err
}

func (db *DB) DeleteProcess(chatId int64) error {
	_, err := db.conn.Exec(`DELETE FROM processes WHERE chat_id = ?`, chatId)
	return err
}

func (db *DB) GetAllProcesses() ([]Process, error) {
	rows, err := db.conn.Query(`SELECT chat_id, process_key, process_data, last_action_at FROM processes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var processes []Process
	for rows.Next() {
		var process Process
		if err := rows.Scan(&process.ChatId, &process.ProcessKey, &process.ProcessData, &process.LastActionAt); err != nil {
			return nil, err
		}
		processes = append(processes, process)
	}
	return processes, rows.Err()
}

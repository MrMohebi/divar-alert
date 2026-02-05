package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const historyDir = "history"

// EnsureHistoryDir creates the history directory if it doesn't exist
func EnsureHistoryDir() error {
	if _, err := os.Stat(historyDir); os.IsNotExist(err) {
		return os.MkdirAll(historyDir, 0755)
	}
	return nil
}

// GetHistoryFilePath returns the path to the history file for a given alert
func GetHistoryFilePath(alertID int64) string {
	return filepath.Join(historyDir, fmt.Sprintf("posts_history_%d.json", alertID))
}

// LoadHistory loads the post history for a given alert
func LoadHistory(alertID int64) ([]interface{}, error) {
	filePath := GetHistoryFilePath(alertID)

	// If file doesn't exist, return empty history
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return []interface{}{}, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read history file: %w", err)
	}

	var history []interface{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &history); err != nil {
			return nil, fmt.Errorf("failed to unmarshal history: %w", err)
		}
	}

	return history, nil
}

// SaveHistory saves the updated history for a given alert
func SaveHistory(alertID int64, history []interface{}) error {
	if err := EnsureHistoryDir(); err != nil {
		return fmt.Errorf("failed to ensure history directory: %w", err)
	}

	filePath := GetHistoryFilePath(alertID)

	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write history file: %w", err)
	}

	return nil
}

// AppendToHistory adds a new post to the history and saves it
func AppendToHistory(alertID int64, post interface{}) error {
	history, err := LoadHistory(alertID)
	if err != nil {
		return err
	}

	history = append(history, post)

	return SaveHistory(alertID, history)
}

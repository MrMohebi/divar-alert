package divar

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// saveCatalogJSON replaces a catalog only after encoding and writing succeed.
func saveCatalogJSON(path string, catalog any) error {
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return fmt.Errorf("encode catalog: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create catalog directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".catalog-*.json")
	if err != nil {
		return fmt.Errorf("create catalog file: %w", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0644); err != nil {
		return fmt.Errorf("set catalog file permissions: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write catalog: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close catalog file: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace catalog file: %w", err)
	}
	return nil
}

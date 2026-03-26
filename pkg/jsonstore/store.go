package jsonstore

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func HardReset(configDir, defaultsDir string) error {
	entries, err := os.ReadDir(configDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading config dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		os.Remove(filepath.Join(configDir, e.Name()))
	}

	if defaultsDir == "" {
		return nil
	}

	defaults, err := os.ReadDir(defaultsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading defaults dir: %w", err)
	}
	for _, e := range defaults {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		src := filepath.Join(defaultsDir, e.Name())
		dst := filepath.Join(configDir, e.Name())
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		os.WriteFile(dst, data, 0644)
	}
	return nil
}

func HardResetEmbedded(configDir string, defaultsFS fs.FS) error {
	entries, err := os.ReadDir(configDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading config dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		os.Remove(filepath.Join(configDir, e.Name()))
	}

	if defaultsFS == nil {
		return nil
	}

	fs.WalkDir(defaultsFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, err := fs.ReadFile(defaultsFS, path)
		if err != nil {
			return nil
		}
		os.WriteFile(filepath.Join(configDir, filepath.Base(path)), data, 0644)
		return nil
	})
	return nil
}

func ReadJSON(filePath string, v any) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, v)
}

func WriteJSON(filePath string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

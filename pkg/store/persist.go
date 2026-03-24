package store

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
)

func saveGob(path string, data any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	if err := gob.NewEncoder(f).Encode(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("encoding data: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

func loadGob(path string, dest any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return gob.NewDecoder(bufio.NewReaderSize(f, 256*1024)).Decode(dest)
}

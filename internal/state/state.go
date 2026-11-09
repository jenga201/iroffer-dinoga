// Package state persists bot state across restarts: the pack listing and
// per-file resume offsets (the iroffer "statefile"). It is intentionally
// dependency-free; callers build File values from their own types.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// PackRecord is one persisted pack-listing entry.
type PackRecord struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Desc   string `json:"desc,omitempty"`
	Sent   int64  `json:"sent,omitempty"` // cumulative bytes transferred for this pack
}

// File is the persisted state payload.
type File struct {
	Packs     []PackRecord     `json:"packs"`
	Extras    []PackRecord     `json:"extras,omitempty"`
	Excluded  []string         `json:"excluded,omitempty"`
	Resume    map[string]int64 `json:"resume,omitempty"`
	TotalSent int64            `json:"total_sent,omitempty"` // cumulative bytes transferred
}

// Load reads and decodes the state file at path. A missing file returns an
// empty File with no error.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{Resume: map[string]int64{}}, nil
		}
		return nil, err
	}
	f := &File{}
	if err := json.Unmarshal(b, f); err != nil {
		return nil, err
	}
	if f.Resume == nil {
		f.Resume = map[string]int64{}
	}
	return f, nil
}

// Save marshals f as JSON and writes it to path atomically (temp file + rename).
func Save(path string, f *File) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

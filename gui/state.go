package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/thgossler/mdv/internal/core"
)

// WindowState is the persisted window geometry restored across runs.
type WindowState struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximized bool `json:"maximized"`
	Valid     bool `json:"valid"`
}

func windowStatePath() (string, error) {
	dir, err := core.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "window-state.json"), nil
}

// LoadWindowState reads the saved window geometry, or returns an invalid state.
func LoadWindowState() WindowState {
	path, err := windowStatePath()
	if err != nil {
		return WindowState{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return WindowState{}
	}
	var st WindowState
	if json.Unmarshal(raw, &st) != nil {
		return WindowState{}
	}
	return st
}

// SaveWindowState persists the window geometry.
func SaveWindowState(st WindowState) {
	path, err := windowStatePath()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	st.Valid = true
	if raw, err := json.MarshalIndent(st, "", "  "); err == nil {
		_ = os.WriteFile(path, raw, 0o644)
	}
}

package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCopyFailurePreservesSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unsupported.bin")
	content := []byte{0, 1, 2, 3}
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	timers := map[string]*time.Timer{path: nil}
	var mu sync.Mutex
	onWriteTimerEnd(path, slog.New(slog.NewTextHandler(io.Discard, nil)), &mu, timers)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("source file was not preserved: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("source contents changed: %q", got)
	}
	if _, ok := timers[path]; ok {
		t.Fatal("completed timer was not removed")
	}
}

func TestCopyMissingFileReturnsError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := addFileToClipboard(filepath.Join(t.TempDir(), "missing"), logger); err == nil {
		t.Fatal("expected an error for a missing source file")
	}
}

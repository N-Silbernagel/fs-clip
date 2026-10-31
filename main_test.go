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

func TestEnsureDir(t *testing.T) {
	t.Run("creates missing directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "watched")
		if err := ensureDir(path); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Fatal("watch path is not a directory")
		}
	})

	t.Run("accepts existing directory without changing contents", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "existing.txt")
		if err := os.WriteFile(path, []byte("keep me"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := ensureDir(dir); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "keep me" {
			t.Fatalf("existing file changed: %q", content)
		}
	})

	t.Run("rejects existing file without changing contents", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(path, []byte("keep me"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := ensureDir(path); err == nil {
			t.Fatal("expected an error for a regular file")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "keep me" {
			t.Fatalf("existing file changed: %q", content)
		}
	})
}

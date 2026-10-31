package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type logMessages chan string

func (messages logMessages) Write(data []byte) (int, error) {
	messages <- string(data)
	return len(data), nil
}

func TestWritesPostponeClipboardAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "unsupported.bin")
		if err := os.WriteFile(path, []byte{0, 1, 2, 3}, 0600); err != nil {
			t.Fatal(err)
		}
		// Unsupported content makes copy attempts observable without a desktop clipboard.
		output := make(logMessages, 10)
		logger := slog.New(slog.NewTextHandler(output, nil))
		var mu sync.Mutex
		timers := make(map[string]*time.Timer)
		const delay = 100 * time.Millisecond
		handleFileCreateEvent(delay, path, logger, &mu, timers)
		for range 3 {
			time.Sleep(75 * time.Millisecond)
			synctest.Wait()
			if len(output) != 0 {
				t.Fatalf("Copy attempted while writes continued: %s", <-output)
			}
			handleFileWriteEvent(delay, path, &mu, timers)
		}
		time.Sleep(delay - time.Nanosecond)
		synctest.Wait()
		if len(output) != 0 {
			t.Fatalf("Copy attempted before the final idle interval: %s", <-output)
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if len(output) != 1 {
			t.Fatalf("Expected one copy attempt after writes stopped; got %d", len(output))
		}
		if message := <-output; !strings.Contains(message, "unsupported content type") {
			t.Fatalf("Unexpected log message: %s", message)
		}
		time.Sleep(delay)
		synctest.Wait()
		if len(output) != 0 {
			t.Fatalf("Unexpected repeated copy attempt: %s", <-output)
		}
	})
}

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

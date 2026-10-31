//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func startApplication(t *testing.T, binary, watched string) string {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "fs-clip.log")
	log, err := os.Create(logPath)
	requireSuccess(t, err)
	cmd := exec.Command(binary, "-watch-dir", watched, "-log-level", "DEBUG")
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
		_ = log.Close()
		diagnostics := readFile(t, logPath)
		t.Logf("fs-clip output:\n%s", diagnostics)
		if bytes.Contains(diagnostics, []byte("WARNING: DATA RACE")) {
			t.Error("Application race detector reported a data race")
		}
	})
	waitFor(t, "watcher readiness", func() bool {
		select {
		case err := <-done:
			t.Fatalf("fs-clip exited before watcher readiness: %v\n%s", err, readFile(t, logPath))
		default:
		}
		return bytes.Contains(readFile(t, logPath), []byte("Watching directory"))
	})
	return logPath
}

func testClipboard(t *testing.T, binary string) {
	t.Helper()
	sentinel := "fs-clip clipboard smoke check"
	writeClipboard(t, sentinel)
	waitFor(t, "native clipboard write/read", func() bool { return readClipboard(t) == sentinel })
	watched := t.TempDir()
	log := startApplication(t, binary, watched)

	copied := func(t *testing.T, path, text string) {
		t.Helper()
		waitFor(t, "source removal", func() bool { _, err := os.Stat(path); return os.IsNotExist(err) })
		waitFor(t, "complete clipboard contents", func() bool { return readClipboard(t) == text })
	}
	plain := filepath.Join(watched, "plain.txt")
	t.Run("UnicodeText", func(t *testing.T) {
		text := "Hello clipboard — Grüße, 世界!\nSecond line.\n"
		writeFile(t, plain, []byte(text), 0600)
		copied(t, plain, text)
	})
	if t.Failed() {
		return
	}
	var chunked strings.Builder
	t.Run("ChunkedWrite", func(t *testing.T) {
		path := filepath.Join(watched, "chunked.txt")
		file, err := os.Create(path)
		requireSuccess(t, err)
		defer file.Close()
		// File writes are unbuffered. Syncs and sleeps can exceed the idle cutoff;
		// timer resets are covered separately with testing/synctest.
		for i := 0; i < 15; i++ {
			chunk := fmt.Sprintf("chunk %d: complete clipboard content\n", i)
			_, err := file.WriteString(chunk)
			requireSuccess(t, err)
			chunked.WriteString(chunk)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("Source disappeared before writing finished: %v", err)
			}
		}
		requireSuccess(t, file.Close())
		copied(t, path, chunked.String())
	})
	if t.Failed() {
		return
	}
	t.Run("UnsupportedFile", func(t *testing.T) {
		path := filepath.Join(watched, "unsupported.bin")
		data := []byte{0, 1, 2, 3}
		writeFile(t, path, data, 0600)
		waitFor(t, "unsupported file rejection", func() bool { return bytes.Contains(readFile(t, log), []byte("unsupported content type")) })
		assertContents(t, path, data)
		if readClipboard(t) != chunked.String() {
			t.Fatal("Unsupported file changed clipboard contents")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("PNGImage", func(t *testing.T) {
		expected := image.NewRGBA(image.Rect(0, 0, 2, 2))
		colors := []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 255, 255}}
		for i, c := range colors {
			expected.SetRGBA(i%2, i/2, c)
		}
		var data bytes.Buffer
		requireSuccess(t, png.Encode(&data, expected))
		path := filepath.Join(watched, "image.png")
		writeFile(t, path, data.Bytes(), 0600)
		waitFor(t, "image source removal", func() bool { _, err := os.Stat(path); return os.IsNotExist(err) })
		actual := readClipboardImage(t)
		if actual.Bounds().Size() != expected.Bounds().Size() {
			t.Fatal("Clipboard image dimensions differ")
		}
		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				r, g, b, _ := actual.At(actual.Bounds().Min.X+x, actual.Bounds().Min.Y+y).RGBA()
				er, eg, eb, _ := expected.At(x, y).RGBA()
				if r != er || g != eg || b != eb {
					t.Fatalf("Clipboard image pixel differs at (%d,%d)", x, y)
				}
			}
		}
	})
	if t.Failed() {
		return
	}
	t.Run("ReusedPathReplacesImage", func(t *testing.T) {
		text := "Next copy replaces the image clipboard contents."
		writeFile(t, plain, []byte(text), 0600)
		copied(t, plain, text)
	})
}

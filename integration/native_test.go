//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/png"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf16"
)

func command(t *testing.T, input []byte, name string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	return commandOutput(t, input, cmd)
}

func commandOutput(t *testing.T, input []byte, cmd *exec.Cmd) []byte {
	t.Helper()
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %v\n%s", cmd.Args, err, stderr.String())
	}
	return output
}

func powershellArgs(script string) []string {
	words := utf16.Encode([]rune("$ProgressPreference='SilentlyContinue'; " + script))
	data := make([]byte, len(words)*2)
	for i, word := range words {
		binary.LittleEndian.PutUint16(data[i*2:], word)
	}
	return []string{"-NoProfile", "-NonInteractive", "-STA", "-OutputFormat", "Text", "-EncodedCommand", base64.StdEncoding.EncodeToString(data)}
}

func powershellCommand(ctx context.Context, script string, env []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "powershell.exe", powershellArgs(script)...)
	// Windows PowerShell must rebuild its module path when launched through Go
	// from PowerShell 7. Inheriting PS7's path can hide built-in cmdlets.
	cmd.Env = make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "PSModulePath") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	return cmd
}

func powershell(t *testing.T, script string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := powershellCommand(ctx, "$ErrorActionPreference='Stop'; "+script, os.Environ())
	return commandOutput(t, nil, cmd)
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func writeClipboard(t *testing.T, text string) {
	t.Helper()
	switch runtime.GOOS {
	case "darwin":
		command(t, []byte(text), "pbcopy")
	case "windows":
		powershell(t, "Set-Clipboard -Value ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('"+base64.StdEncoding.EncodeToString([]byte(text))+"')))")
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
		// The forked selection owner must not inherit Go's output pipes.
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
}

func readClipboard(t *testing.T) string {
	t.Helper()
	switch runtime.GOOS {
	case "darwin":
		return string(command(t, nil, "pbpaste"))
	case "windows":
		output := powershell(t, "$text=Get-Clipboard -Raw; [Console]::Write([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($text)))")
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(output)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	default:
		return string(command(t, nil, "xclip", "-selection", "clipboard", "-o"))
	}
}

func readClipboardImage(t *testing.T) image.Image {
	t.Helper()
	var data []byte
	switch runtime.GOOS {
	case "darwin":
		output := strings.TrimSpace(string(command(t, nil, "osascript", "-e", "get the clipboard as «class PNGf»")))
		if !strings.HasPrefix(output, "«data PNGf") || !strings.HasSuffix(output, "»") {
			t.Fatalf("Unexpected image response: %.100s", output)
		}
		encoded := strings.TrimSuffix(strings.TrimPrefix(output, "«data PNGf"), "»")
		encoded = strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, encoded)
		var err error
		data, err = hex.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
	case "windows":
		output := powershell(t, "Add-Type -AssemblyName System.Windows.Forms; "+
			"$image=[Windows.Forms.Clipboard]::GetImage(); "+
			"if ($null -eq $image) { throw 'Clipboard has no image' }; "+
			"$stream=New-Object IO.MemoryStream; try { "+
			"$image.Save($stream, [Drawing.Imaging.ImageFormat]::Png); "+
			"[Console]::Write([Convert]::ToBase64String($stream.ToArray())) "+
			"} finally { $stream.Dispose(); $image.Dispose() }")
		var err error
		data, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(output)))
		if err != nil {
			t.Fatal(err)
		}
	default:
		data = command(t, nil, "xclip", "-selection", "clipboard", "-o", "-target", "image/png")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func waitFor(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Timed out waiting for %s", description)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func assertContents(t *testing.T, path string, expected []byte) {
	t.Helper()
	if !bytes.Equal(readFile(t, path), expected) {
		t.Fatalf("Contents changed: %s", path)
	}
}

func requireSuccess(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func managementEnvironment(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "localappdata"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	dir, err := stateDirectory()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPortableConfiguration(t *testing.T) {
	dir := managementEnvironment(t)
	watched := filepath.Join(t.TempDir(), "watch & copy", "nested")
	var output bytes.Buffer
	if handled, err := managementCommand([]string{"configure", "--watch-dir", watched}, &output); !handled || err != nil {
		t.Fatalf("configure: handled=%t, error=%v", handled, err)
	}
	if got := determineWatchPath(""); got != watched {
		t.Fatalf("Default worker path = %q, expected %q", got, watched)
	}
	if _, err := os.Stat(watched); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if _, err := managementCommand([]string{"status"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), watched) || !strings.Contains(output.String(), "not configured") {
		t.Fatalf("Unexpected status: %s", output.String())
	}
	// Configuring a portable executable must not implicitly register automatic startup.
	if _, err := os.Stat(filepath.Join(dir, "installation.json")); !os.IsNotExist(err) {
		t.Fatalf("Unexpected installation record: %v", err)
	}
	invalid := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(invalid, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := managementCommand([]string{"configure", "--watch-dir", invalid}, io.Discard); err == nil {
		t.Fatal("Configuration accepted a regular file")
	}
	if got := determineWatchPath(""); got != watched {
		t.Fatalf("Rejected configuration changed watch directory: %q", got)
	}
	if _, err := managementCommand([]string{"uninstall"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := determineWatchPath(""); got != watched {
		t.Fatalf("Uninstall erased portable preferences: %q", got)
	}
}

func TestExplicitWorkerPathDoesNotReadSavedSettings(t *testing.T) {
	dir := managementEnvironment(t)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	watched := t.TempDir()
	if got := determineWatchPath(watched); got != watched {
		t.Fatalf("Explicit watch directory = %q", got)
	}
}

func TestManagementRejectsInvalidCommands(t *testing.T) {
	managementEnvironment(t)
	for _, args := range [][]string{
		{"unknown"}, {"configure"}, {"configure", "--watch-dir", "x", "unexpected"},
		{"install", "--watch-dir", "x"}, {"configure", "--watch-dir", "bad\npath"},
		{"configure", "--watch-dir", string([]byte{0xff})}, {"status", "unexpected"}, {"uninstall", "--purge"},
	} {
		if handled, err := managementCommand(args, io.Discard); !handled || err == nil {
			t.Errorf("Arguments %v: handled=%t, error=%v", args, handled, err)
		}
	}
}

func TestManagementLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "management.lock")
	first, err := acquireManagementLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := acquireManagementLock(path); err == nil {
		second.Close()
		t.Fatal("Concurrent management lock was accepted")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := acquireManagementLock(path)
	if err != nil {
		t.Fatalf("Lock remained held after close: %v", err)
	}
	third.Close()
}

func TestExecutableReplacementRollback(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "downloaded")
	target := filepath.Join(dir, executableName())
	for path, value := range map[string]string{source: "new executable", target: "old executable"} {
		if err := os.WriteFile(path, []byte(value), 0755); err != nil {
			t.Fatal(err)
		}
	}
	undo, _, err := replaceExecutable(source, target)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new executable" {
		t.Fatalf("Replacement: %s, %v", data, err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "old executable" {
		t.Fatalf("Rollback: %s, %v", data, err)
	}
}

func TestLegacySettingsMigration(t *testing.T) {
	for _, format := range []string{"shell", "powershell"} {
		t.Run(format, func(t *testing.T) {
			dir := managementEnvironment(t)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			watched := filepath.Join(t.TempDir(), "saved watch")
			binDir := filepath.Join(t.TempDir(), "custom bin")
			if format == "shell" {
				for name, value := range map[string]string{"watch-dir": watched, "install-dir": binDir} {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				data, err := json.Marshal(map[string]any{"WatchDir": watched, "InstallDir": binDir, "Installed": true, "PathAdded": true})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "settings.json"), append([]byte{0xef, 0xbb, 0xbf}, data...), 0600); err != nil {
					t.Fatal(err)
				}
			}
			m := manager{dir: dir, out: io.Discard}
			p, err := m.loadPreferences()
			if err != nil || p.WatchDir != watched {
				t.Fatalf("Preferences = %+v, %v", p, err)
			}
			inst, err := m.loadInstallation()
			if err != nil || inst == nil || inst.Binary != filepath.Join(binDir, executableName()) {
				t.Fatalf("Installation = %+v, %v", inst, err)
			}
		})
	}
}

func TestExternallyManagedInstallationIsProtected(t *testing.T) {
	dir := managementEnvironment(t)
	if err := writeJSON(filepath.Join(dir, "installation.json"), installation{Method: "homebrew", Binary: filepath.Join(t.TempDir(), executableName())}); err != nil {
		t.Fatal(err)
	}
	if _, err := managementCommand([]string{"uninstall"}, io.Discard); err == nil || !strings.Contains(err.Error(), "package manager") {
		t.Fatalf("Uninstall did not protect externally managed installation: %v", err)
	}
}

//go:build integration

package integration

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const label = "dev.nils-silbernagel.fs-clip"

func releaseArchive(t *testing.T, binary, service string) []byte {
	t.Helper()
	var archive bytes.Buffer
	binaryName := "fs-clip"
	if runtime.GOOS == "windows" {
		output := zip.NewWriter(&archive)
		entry, err := output.Create(binaryName + ".exe")
		requireSuccess(t, err)
		_, err = entry.Write(readFile(t, binary))
		requireSuccess(t, err)
		entry, err = output.Create("manage.ps1")
		requireSuccess(t, err)
		_, err = entry.Write(readFile(t, filepath.Join("..", "scripts", "manage.ps1")))
		requireSuccess(t, err)
		requireSuccess(t, output.Close())
	} else {
		compressed := gzip.NewWriter(&archive)
		output := tar.NewWriter(compressed)
		for _, file := range []struct {
			path, name string
			mode       int64
		}{
			{binary, binaryName, 0755}, {service, filepath.Base(service), 0644},
			{filepath.Join("..", "scripts", "manage.sh"), "manage.sh", 0700},
		} {
			data := readFile(t, file.path)
			requireSuccess(t, output.WriteHeader(&tar.Header{Name: file.name, Mode: file.mode, Size: int64(len(data))}))
			_, err := output.Write(data)
			requireSuccess(t, err)
		}
		requireSuccess(t, output.Close())
		requireSuccess(t, compressed.Close())
	}
	return archive.Bytes()
}

func TestInstallation(t *testing.T) {
	repo, err := filepath.Abs("..")
	requireSuccess(t, err)
	root := t.TempDir()
	binaryName := "fs-clip"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binary := filepath.Join(root, binaryName)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	buildArgs := []string{"build", "-o", binary}
	if runtime.GOOS == "windows" {
		buildArgs = append(buildArgs, "-ldflags=-H=windowsgui")
	}
	build := exec.CommandContext(ctx, "go", append(buildArgs, ".")...)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	build.Dir = repo
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("Build application: %v\n%s", err, output)
	}
	info, err := buildinfo.ReadFile(binary)
	requireSuccess(t, err)
	cgoDisabled := false
	for _, setting := range info.Settings {
		if setting.Key == "CGO_ENABLED" {
			cgoDisabled = setting.Value == "0"
		}
	}
	if !cgoDisabled {
		t.Fatal("Application binary does not match release build settings")
	}
	original := readFile(t, binary)
	home := filepath.Join(root, "home with spaces")
	bindir := filepath.Join(home, "custom bin")
	watchdir := filepath.Join(home, "watch & copy")
	installed := filepath.Join(bindir, binaryName)
	web := filepath.Join(root, "web")
	const version = "0.0.0-test"
	const tag = "v" + version
	downloads := filepath.Join(web, "download", tag)
	requireSuccess(t, os.MkdirAll(downloads, 0755))
	requireSuccess(t, os.MkdirAll(home, 0755))
	extension, service := "tar.gz", label+".service"
	switch runtime.GOOS {
	case "windows":
		extension = "zip"
	case "darwin":
		service = label + ".plist"
	case "linux":
	default:
		t.Fatalf("Unsupported test platform: %s", runtime.GOOS)
	}
	archiveName := fmt.Sprintf("fs-clip_%s_%s_%s.%s", version, runtime.GOOS, runtime.GOARCH, extension)
	archive := releaseArchive(t, binary, filepath.Join(repo, service))
	writeFile(t, filepath.Join(downloads, archiveName), archive, 0600)
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	checksums := filepath.Join(downloads, fmt.Sprintf("fs-clip_%s_checksums.txt", version))
	writeFile(t, checksums, []byte(digest+"  "+archiveName+"\n"), 0600)
	files := http.FileServer(http.Dir(web))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" || r.URL.Path == "/"+tag {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag})
			return
		}
		files.ServeHTTP(w, r)
	}))
	defer server.Close()
	commands := filepath.Join(root, "commands")
	requireSuccess(t, os.MkdirAll(commands, 0755))
	serviceLog := filepath.Join(root, "service.log")
	if runtime.GOOS != "windows" {
		// Downloads and installation are real; service activation is recorded so
		// the test never registers persistent services in the runner's session.
		stub := `#!/bin/sh
printf '%s\n' "$*" >> "$SERVICE_LOG"
case "$*" in
  *" cat "*) test -f "$SERVICE_LOG.loaded";;
  print*) test -f "$SERVICE_LOG.loaded";;
  *"enable --now"*|"bootstrap "*)
    if test -f "$FAIL_SERVICE_ONCE"; then rm "$FAIL_SERVICE_ONCE"; exit 1; fi
    touch "$SERVICE_LOG.loaded";;
  *"disable --now"*|"bootout "*) rm -f "$SERVICE_LOG.loaded";;
esac
`
		for _, name := range []string{"systemctl", "launchctl"} {
			writeFile(t, filepath.Join(commands, name), []byte(stub), 0755)
		}
	}
	env := append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"LOCALAPPDATA="+filepath.Join(home, "localappdata"), "SERVICE_LOG="+serviceLog,
		"FAIL_SERVICE_ONCE="+filepath.Join(root, "fail-start-once"),
		"PATH="+commands+string(os.PathListSeparator)+os.Getenv("PATH"), "FS_CLIP_RELEASE_BASE_URL="+server.URL)

	if runtime.GOOS == "windows" {
		prepareWindowsTask(t)
		originalPath := string(powershell(t, "[Console]::Write([Environment]::GetEnvironmentVariable('Path','User'))"))
		t.Cleanup(func() {
			powershell(t, "[Environment]::SetEnvironmentVariable('Path',"+psQuote(originalPath)+",'User')")
		})
	}

	install := func(t *testing.T, requestedTag string, success bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			invocation := fmt.Sprintf("& %s -InstallDir %s -WatchDir %s -Tag %s -ReleaseBaseUrl %s\n",
				psQuote(filepath.Join(repo, "scripts", "install.ps1")), psQuote(bindir), psQuote(watchdir), psQuote(requestedTag), psQuote(server.URL))
			repeat := ""
			if success {
				repeat = invocation
			}
			driver := "$ErrorActionPreference='Stop'; " +
				"$originalPath=[Environment]::GetEnvironmentVariable('Path','User'); try {\n" +
				invocation + repeat +
				"$entries=[Environment]::GetEnvironmentVariable('Path','User') -split ';'; " +
				"if (@($entries | Where-Object { $_ -eq " + psQuote(bindir) + " }).Count -ne 1) { throw 'PATH entry is missing or duplicated' }\n" +
				"} finally { [Environment]::SetEnvironmentVariable('Path',$originalPath,'User') }"
			cmd = powershellCommand(ctx, driver, env)
		} else {
			cmd = exec.CommandContext(ctx, "sh", filepath.Join(repo, "scripts", "install.sh"), "-b", bindir, "-w", watchdir, "-t", requestedTag)
			cmd.Env = env
		}
		output, err := cmd.CombinedOutput()
		t.Logf("Installer output:\n%s", output)
		if (err == nil) != success {
			t.Fatalf("Installer error=%v; expected success=%t", err, success)
		}
		if !success && !bytes.Contains(bytes.ToLower(output), []byte("checksum")) {
			t.Fatal("Installer failed without reporting checksum rejection")
		}
	}

	if !t.Run("Latest", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			requireSuccess(t, os.MkdirAll(bindir, 0755))
			writeFile(t, installed, original, 0755)
			startApplication(t, installed, watchdir)
		}
		install(t, "latest", true)
		assertContents(t, installed, original)
		command(t, nil, installed, "-h")
		switch runtime.GOOS {
		case "windows":
			assertWindowsTask(t, installed, watchdir)
		case "darwin":
			config := filepath.Join(home, "Library", "LaunchAgents", service)
			command(t, nil, "plutil", "-lint", config)
			args := command(t, nil, "plutil", "-extract", "ProgramArguments", "json", "-o", "-", config)
			var actual []string
			requireSuccess(t, json.Unmarshal(args, &actual))
			expected := []string{installed, "--watch-dir", watchdir}
			if len(actual) != len(expected) {
				t.Fatalf("Unexpected LaunchAgent arguments: %q", actual)
			}
			for i := range expected {
				if actual[i] != expected[i] {
					t.Fatalf("Unexpected LaunchAgent arguments: %q", actual)
				}
			}
			if !bytes.Contains(readFile(t, serviceLog), []byte("bootstrap ")) {
				t.Fatal("Installer did not request LaunchAgent loading")
			}
		case "linux":
			config := string(readFile(t, filepath.Join(home, ".config", "systemd", "user", service)))
			if !strings.Contains(config, fmt.Sprintf("ExecStart=%q --watch-dir %q", installed, watchdir)) {
				t.Fatalf("Incorrect service arguments:\n%s", config)
			}
			if strings.Contains(config, "User=") || !strings.Contains(config, "WantedBy=default.target") {
				t.Fatalf("Invalid user service:\n%s", config)
			}
			calls := string(readFile(t, serviceLog))
			for _, call := range strings.Split(strings.TrimSpace(calls), "\n") {
				if !strings.HasPrefix(call, "--user ") {
					t.Fatalf("System-level systemctl call: %s", call)
				}
			}
			if !strings.Contains(calls, "--user daemon-reload\n") || !strings.Contains(calls, "--user enable --now "+service+"\n") {
				t.Fatal("Installer did not reload and enable user service")
			}
		}
	}) {
		return
	}
	if !t.Run("ReinstallExplicitTag", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			startApplication(t, installed, filepath.Join(root, "upgrade-watch"))
		}
		if runtime.GOOS == "windows" {
			watchdir = filepath.Join(home, "updated watch ' & copy") + string(os.PathSeparator)
		}
		install(t, tag, true)
		if runtime.GOOS == "windows" {
			assertWindowsTask(t, installed, watchdir)
		}
		assertContents(t, installed, original)
		if runtime.GOOS != "windows" {
			stop := "bootout "
			if runtime.GOOS == "linux" {
				stop = "--user disable --now " + service
			}
			if !strings.Contains(string(readFile(t, serviceLog)), stop) {
				t.Fatal("Reinstall did not stop existing service")
			}
		}
	}) {
		return
	}
	for _, test := range []struct{ name, checksum string }{
		{"RejectMismatchedChecksum", strings.Repeat("0", 64) + "  " + archiveName + "\n"},
		{"RejectMissingChecksum", digest + "  unrelated-archive.tar.gz\n"},
	} {
		if !t.Run(test.name, func(t *testing.T) {
			writeFile(t, checksums, []byte(test.checksum), 0600)
			install(t, "latest", false)
			assertContents(t, installed, original)
		}) {
			return
		}
	}
	if runtime.GOOS == "windows" {
		t.Run("TaskSurvivesRejectedUpdate", func(t *testing.T) { assertWindowsTask(t, installed, watchdir) })
	}

	t.Run("InstalledClipboard", func(t *testing.T) { testClipboard(t, installed) })

	state := filepath.Join(home, ".config", "fs-clip")
	manager := filepath.Join(state, "manage.sh")
	if runtime.GOOS == "darwin" {
		state = filepath.Join(home, "Library", "Application Support", "fs-clip")
		manager = filepath.Join(state, "manage.sh")
	} else if runtime.GOOS == "windows" {
		state = filepath.Join(home, "localappdata", "fs-clip")
		manager = filepath.Join(state, "manage.ps1")
	}
	runManage := func(t *testing.T, operation string, args ...string) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			invocation := "& " + psQuote(manager) + " " + operation
			if operation == "Configure" {
				invocation += " -WatchDir " + psQuote(args[1])
			}
			cmd = powershellCommand(ctx, invocation, env)
		} else {
			cmd = exec.CommandContext(ctx, "sh", append([]string{manager, strings.ToLower(operation)}, args...)...)
			cmd.Env = env
		}
		return cmd.CombinedOutput()
	}
	manage := func(t *testing.T, operation string, args ...string) []byte {
		t.Helper()
		output, err := runManage(t, operation, args...)
		if err != nil {
			t.Fatalf("Management %s: %v\n%s", operation, err, output)
		}
		return output
	}
	t.Run("OfflineConfigureAndStatus", func(t *testing.T) {
		watchdir = filepath.Join(home, "configured ' & watch")
		flag := "--watch-dir"
		if runtime.GOOS == "windows" {
			flag = "-WatchDir"
		}
		manage(t, "Configure", flag, watchdir)
		output := manage(t, "Status")
		if !bytes.Contains(output, []byte(watchdir)) || !bytes.Contains(output, []byte(installed)) {
			t.Fatalf("Status omitted installation settings: %s", output)
		}
		if runtime.GOOS == "windows" {
			assertWindowsTask(t, installed, watchdir)
		}
	})
	t.Run("RejectInvalidConfiguration", func(t *testing.T) {
		invalid := filepath.Join(home, "existing-file")
		writeFile(t, invalid, []byte("keep"), 0600)
		flag := "--watch-dir"
		if runtime.GOOS == "windows" {
			flag = "-WatchDir"
		}
		if output, err := runManage(t, "Configure", flag, invalid); err == nil {
			t.Fatalf("Configuration accepted a regular file: %s", output)
		}
		if !bytes.Contains(manage(t, "Status"), []byte(watchdir)) {
			t.Fatal("Rejected configuration changed saved preferences")
		}
	})
	if runtime.GOOS != "windows" {
		t.Run("ConfigureActivationFailureRestoresSettings", func(t *testing.T) {
			writeFile(t, filepath.Join(root, "fail-start-once"), []byte("fail"), 0600)
			if output, err := runManage(t, "Configure", "--watch-dir", filepath.Join(home, "failed-watch")); err == nil {
				t.Fatalf("Configuration ignored service activation failure: %s", output)
			}
			if !bytes.Contains(manage(t, "Status"), []byte(watchdir)) {
				t.Fatal("Failed activation changed saved preferences")
			}
			assertContents(t, filepath.Join(state, "watch-dir"), []byte(watchdir+"\n"))
			config := filepath.Join(home, ".config", "systemd", "user", service)
			if runtime.GOOS == "darwin" {
				config = filepath.Join(home, "Library", "LaunchAgents", service)
			}
			if bytes.Contains(readFile(t, config), []byte("failed-watch")) {
				t.Fatal("Failed configuration remains in service definition")
			}
		})
		t.Run("ConcurrentManagementRejected", func(t *testing.T) {
			lock := filepath.Join(state, "lock")
			requireSuccess(t, os.Mkdir(lock, 0700))
			defer os.Remove(lock)
			if output, err := runManage(t, "Configure", "--watch-dir", filepath.Join(home, "concurrent-watch")); err == nil {
				t.Fatalf("Concurrent management was accepted: %s", output)
			}
			assertContents(t, filepath.Join(state, "watch-dir"), []byte(watchdir+"\n"))
		})
	}
	t.Run("ReinstallPreservesSettings", func(t *testing.T) {
		writeFile(t, checksums, []byte(digest+"  "+archiveName+"\n"), 0600)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = powershellCommand(ctx, "& "+psQuote(filepath.Join(repo, "scripts", "install.ps1"))+" -ReleaseBaseUrl "+psQuote(server.URL), env)
		} else {
			cmd = exec.CommandContext(ctx, "sh", filepath.Join(repo, "scripts", "install.sh"))
			cmd.Env = env
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Reinstall: %v\n%s", err, output)
		}
		assertContents(t, installed, original)
		if !bytes.Contains(manage(t, "Status"), []byte(watchdir)) {
			t.Fatal("Reinstall lost configured watch directory")
		}
	})
	t.Run("UninstallPreservesWatchedFiles", func(t *testing.T) {
		// Unsupported contents survive even while the real Windows task is running.
		kept := filepath.Join(watchdir, "keep.bin")
		contents := []byte{0, 1, 2, 3}
		writeFile(t, kept, contents, 0600)
		manage(t, "Uninstall")
		assertContents(t, kept, contents)
		if _, err := os.Stat(installed); !os.IsNotExist(err) {
			t.Fatalf("Executable remains after uninstall: %v", err)
		}
		if runtime.GOOS == "windows" {
			powershell(t, windowsTaskIdentity+`if (Get-ScheduledTask | Where-Object { $_.TaskName -eq $taskName }) { throw 'Task remains after uninstall' }`)
		} else {
			config := filepath.Join(home, ".config", "systemd", "user", service)
			if runtime.GOOS == "darwin" {
				config = filepath.Join(home, "Library", "LaunchAgents", service)
			}
			if _, err := os.Stat(config); !os.IsNotExist(err) {
				t.Fatalf("Startup definition remains after uninstall: %v", err)
			}
		}
		manage(t, "Uninstall")
	})
}

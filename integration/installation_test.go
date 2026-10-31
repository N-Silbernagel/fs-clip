//go:build integration

package integration

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
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
		requireSuccess(t, output.Close())
	} else {
		compressed := gzip.NewWriter(&archive)
		output := tar.NewWriter(compressed)
		for _, file := range []struct {
			path, name string
			mode       int64
		}{
			{binary, binaryName, 0755}, {service, filepath.Base(service), 0644},
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
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, ".")
	build.Dir = repo
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("Build application: %v\n%s", err, output)
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
	extension, service, checksumSuffix := "tar.gz", label+".service", ""
	switch runtime.GOOS {
	case "windows":
		extension = "zip"
	case "darwin":
		service = label + ".plist"
	case "linux":
		checksumSuffix = "_linux"
	default:
		t.Fatalf("Unsupported test platform: %s", runtime.GOOS)
	}
	archiveName := fmt.Sprintf("fs-clip_%s_%s_%s.%s", version, runtime.GOOS, runtime.GOARCH, extension)
	archive := releaseArchive(t, binary, filepath.Join(repo, service))
	writeFile(t, filepath.Join(downloads, archiveName), archive, 0600)
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	checksums := filepath.Join(downloads, fmt.Sprintf("fs-clip_%s_checksums%s.txt", version, checksumSuffix))
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
  list) test -f "$SERVICE_LOG.loaded" && echo '` + label + `';;
  *"enable --now"*|"load "*) touch "$SERVICE_LOG.loaded";;
  *"disable --now"*|"unload "*) rm -f "$SERVICE_LOG.loaded";;
esac
`
		for _, name := range []string{"systemctl", "launchctl"} {
			writeFile(t, filepath.Join(commands, name), []byte(stub), 0755)
		}
	}
	env := append(os.Environ(), "HOME="+home, "SERVICE_LOG="+serviceLog,
		"PATH="+commands+string(os.PathListSeparator)+os.Getenv("PATH"), "FS_CLIP_RELEASE_BASE_URL="+server.URL)

	install := func(t *testing.T, requestedTag string, success bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			invocation := fmt.Sprintf("& %s -InstallDir %s -WatchDir %s -Tag %s -ReleaseBaseUrl %s -NoStart\n",
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
			cmd = exec.CommandContext(ctx, "powershell.exe", powershellArgs(driver)...)
		} else {
			cmd = exec.CommandContext(ctx, "sh", filepath.Join(repo, "scripts", "install.sh"), "-b", bindir, "-w", watchdir, "-t", requestedTag)
		}
		cmd.Env = env
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
		install(t, "latest", true)
		assertContents(t, installed, original)
		command(t, nil, installed, "-h")
		switch runtime.GOOS {
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
			if !bytes.Contains(readFile(t, serviceLog), []byte("load ")) {
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
		install(t, tag, true)
		assertContents(t, installed, original)
		if runtime.GOOS != "windows" {
			stop := "unload "
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
	t.Run("InstalledClipboard", func(t *testing.T) { testClipboard(t, installed) })
}

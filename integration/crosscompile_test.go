//go:build crosscompile

package integration

import (
	"context"
	"debug/buildinfo"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCrossCompileReleaseTargets(t *testing.T) {
	for _, target := range []struct{ os, arch string }{
		{"linux", "amd64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
		{"windows", "amd64"},
		{"windows", "arm64"},
	} {
		t.Run(target.os+"/"+target.arch, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			binary := filepath.Join(t.TempDir(), "fs-clip")
			args := []string{"build", "-o", binary}
			if target.os == "windows" {
				args = append(args, "-ldflags=-H=windowsgui")
			}
			cmd := exec.CommandContext(ctx, "go", append(args, ".")...)
			cmd.Dir = ".."
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+target.os, "GOARCH="+target.arch)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("Release target build failed: %v\n%s", err, output)
			}
			if target.os == "windows" {
				executable, err := pe.Open(binary)
				if err != nil {
					t.Fatal(err)
				}
				defer executable.Close()
				if executable.OptionalHeader.(*pe.OptionalHeader64).Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI {
					t.Fatal("Windows release binary would open a console window")
				}
			}
			info, err := buildinfo.ReadFile(binary)
			if err != nil {
				t.Fatal(err)
			}
			settings := make(map[string]string)
			for _, setting := range info.Settings {
				settings[setting.Key] = setting.Value
			}
			if settings["CGO_ENABLED"] != "0" || settings["GOOS"] != target.os || settings["GOARCH"] != target.arch {
				t.Fatalf("Unexpected binary build settings: %v", settings)
			}
		})
	}
}

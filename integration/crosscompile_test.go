//go:build crosscompile

package integration

import (
	"context"
	"debug/buildinfo"
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
			cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
			cmd.Dir = ".."
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+target.os, "GOARCH="+target.arch)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("CGO-free build failed: %v\n%s", err, output)
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

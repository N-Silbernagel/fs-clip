package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const serviceLabel = "dev.nils-silbernagel.fs-clip"

type preferences struct {
	WatchDir string `json:"watch_dir"`
}

type installationMethod string

const (
	installationMethodScript installationMethod = "script"
	installationMethodInno   installationMethod = "inno"
)

type installation struct {
	Binary    string             `json:"binary"`
	Method    installationMethod `json:"method"`
	PathAdded bool               `json:"path_added,omitempty"`
}

type manager struct {
	dir string
	out io.Writer
}

func managementCommand(args []string, out io.Writer) (bool, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return false, nil
	}
	command := args[0]
	if command != "install" && command != "activate" && command != "configure" && command != "status" && command != "deactivate" && command != "uninstall" {
		return true, fmt.Errorf("unknown command %q; use fs-clip --help", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(out)
	var binDir, watchDir, methodName string
	if command == "install" {
		flags.StringVar(&binDir, "bin-dir", "", "Installation directory (defaults to your user binary directory)")
	}
	if command == "configure" || command == "activate" {
		flags.StringVar(&watchDir, "watch-dir", "", "Directory to watch")
	}
	if command == "activate" || command == "deactivate" {
		flags.StringVar(&methodName, "method", "", "External installer method")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return true, nil
		}
		return true, err
	}
	if flags.NArg() != 0 {
		return true, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if command == "configure" && watchDir == "" {
		return true, errors.New("use fs-clip configure --watch-dir DIRECTORY")
	}
	method := installationMethod(methodName)
	if (command == "activate" || command == "deactivate") && method != installationMethodInno {
		return true, errors.New("installer method must be inno")
	}
	dir, err := stateDirectory()
	if err != nil {
		return true, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return true, err
	}
	lock, err := acquireManagementLock(filepath.Join(dir, "management.lock"))
	if err != nil {
		return true, fmt.Errorf("cannot lock installation; another management operation may be running: %w", err)
	}
	defer lock.Close()
	m := manager{dir: dir, out: out}
	switch command {
	case "install":
		err = m.install(binDir)
	case "activate":
		err = m.activate(method, watchDir)
	case "configure":
		err = m.configure(watchDir)
	case "status":
		err = m.status()
	case "deactivate":
		err = m.deactivate(method)
	case "uninstall":
		err = m.uninstall()
	}
	return true, err
}

func validPath(path string) bool {
	return utf8.ValidString(path) && strings.IndexFunc(path, unicode.IsControl) == -1
}

func normalizedPath(path string) (string, error) {
	if !validPath(path) {
		return "", errors.New("paths must contain valid Unicode and no control characters")
	}
	return filepath.Abs(path)
}

func (m manager) loadPreferences() (preferences, error) {
	var p preferences
	_, err := readJSON(filepath.Join(m.dir, "config.json"), &p)
	if err != nil {
		return p, err
	}
	if p.WatchDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return p, err
		}
		p.WatchDir = filepath.Join(home, "copy-to-clipboard")
	}
	if !filepath.IsAbs(p.WatchDir) || !validPath(p.WatchDir) {
		return p, errors.New("saved watch directory must be an absolute path without control characters")
	}
	return p, nil
}

func (m manager) loadInstallation() (*installation, error) {
	var inst installation
	found, err := readJSON(filepath.Join(m.dir, "installation.json"), &inst)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	if inst.Method != installationMethodScript && inst.Method != installationMethodInno {
		return nil, fmt.Errorf("unsupported installation method %q", inst.Method)
	}
	if !filepath.IsAbs(inst.Binary) || filepath.Base(inst.Binary) != executableName() || !validPath(inst.Binary) {
		return nil, errors.New("invalid saved executable path")
	}
	return &inst, nil
}

func readJSON(path string, target any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	return true, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".fs-clip-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (m manager) install(binDir string) (err error) {
	p, err := m.loadPreferences()
	if err != nil {
		return err
	}
	previous, err := m.loadInstallation()
	if err != nil {
		return err
	}
	if previous != nil && previous.Method != installationMethodScript {
		return errors.New("this installation is managed externally; use its package manager")
	}
	if binDir == "" {
		if previous != nil {
			binDir = filepath.Dir(previous.Binary)
		} else if binDir, err = defaultBinaryDirectory(); err != nil {
			return err
		}
	}
	binDir, err = normalizedPath(binDir)
	if err != nil {
		return err
	}
	target := filepath.Join(binDir, executableName())
	if previous != nil && !samePath(previous.Binary, target) {
		return errors.New("uninstall before changing the binary directory")
	}
	if err := os.MkdirAll(p.WatchDir, 0750); err != nil {
		return err
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}
	source, err := os.Executable()
	if err != nil {
		return err
	}
	if err := platformStop(target); err != nil {
		return err
	}
	undo, commit, err := replaceExecutable(source, target)
	if err != nil {
		if previous != nil {
			err = errors.Join(err, platformActivate(*previous, p))
		}
		return err
	}
	inst := installation{Binary: target, Method: installationMethodScript}
	if previous != nil {
		inst.PathAdded = previous.PathAdded
	}
	success := false
	addedPath := false
	defer func() {
		if success {
			commit()
			return
		}
		err = errors.Join(err, platformRemoveStartup(target), undo())
		if previous != nil {
			err = errors.Join(err, platformActivate(*previous, p))
		}
		if addedPath {
			err = errors.Join(err, platformRemovePath(binDir))
		}
	}()
	if err = platformActivate(inst, p); err != nil {
		return fmt.Errorf("start FSClip: %w", err)
	}
	added, err := platformEnsurePath(binDir)
	if err != nil {
		return err
	}
	addedPath = added
	inst.PathAdded = inst.PathAdded || added
	if err = writeJSON(filepath.Join(m.dir, "config.json"), p); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(m.dir, "installation.json"), inst); err != nil {
		return err
	}
	success = true
	fmt.Fprintf(m.out, "Installed %s\nWatching: %s\nUse fs-clip configure, fs-clip status, or fs-clip uninstall.\n", target, p.WatchDir)
	return nil
}

func (m manager) activate(method installationMethod, watchDir string) (err error) {
	previousInstallation, err := m.loadInstallation()
	if err != nil {
		return err
	}
	if previousInstallation != nil && previousInstallation.Method != method {
		return errors.New("another installation method already manages FSClip; uninstall it first")
	}
	previousPreferences, err := m.loadPreferences()
	if err != nil {
		return err
	}
	p := previousPreferences
	if watchDir != "" {
		p.WatchDir, err = normalizedPath(watchDir)
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(p.WatchDir, 0750); err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	binary, err = normalizedPath(binary)
	if err != nil {
		return err
	}
	inst := installation{Binary: binary, Method: method}
	if previousInstallation != nil {
		if err := platformStop(previousInstallation.Binary); err != nil {
			return err
		}
	} else if err := platformStop(binary); err != nil {
		return err
	}
	activationAttempted := false
	preferencesSaved := false
	defer func() {
		if err == nil {
			return
		}
		if activationAttempted {
			err = errors.Join(err, platformRemoveStartup(binary))
		}
		if preferencesSaved {
			err = errors.Join(err, writeJSON(filepath.Join(m.dir, "config.json"), previousPreferences))
		}
		if previousInstallation != nil {
			err = errors.Join(err, platformActivate(*previousInstallation, previousPreferences))
		}
	}()
	activationAttempted = true
	if err = platformActivate(inst, p); err != nil {
		return fmt.Errorf("start FSClip: %w", err)
	}
	if err = writeJSON(filepath.Join(m.dir, "config.json"), p); err != nil {
		return err
	}
	preferencesSaved = true
	if err = writeJSON(filepath.Join(m.dir, "installation.json"), inst); err != nil {
		return err
	}
	fmt.Fprintf(m.out, "Activated %s\nWatching: %s\n", binary, p.WatchDir)
	return nil
}

func (m manager) configure(path string) (err error) {
	path, err = normalizedPath(path)
	if err != nil {
		return err
	}
	previous, err := m.loadPreferences()
	if err != nil {
		return err
	}
	inst, err := m.loadInstallation()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0750); err != nil {
		return err
	}
	p := preferences{WatchDir: path}
	if inst != nil {
		if err := platformStop(inst.Binary); err != nil {
			return err
		}
		defer func() {
			if err != nil {
				err = errors.Join(err, platformStop(inst.Binary), platformActivate(*inst, previous))
			}
		}()
		if err = platformActivate(*inst, p); err != nil {
			return fmt.Errorf("configuration failed; restoring previous settings: %w", err)
		}
	}
	if err = writeJSON(filepath.Join(m.dir, "config.json"), p); err != nil {
		return err
	}
	fmt.Fprintf(m.out, "Watching: %s\n", path)
	return nil
}

func (m manager) status() error {
	p, err := m.loadPreferences()
	if err != nil {
		return err
	}
	fmt.Fprintf(m.out, "Watching: %s\n", p.WatchDir)
	inst, err := m.loadInstallation()
	if err != nil {
		return err
	}
	if inst == nil {
		fmt.Fprintln(m.out, "Automatic startup is not configured.")
		return nil
	}
	fmt.Fprintf(m.out, "Executable: %s\n", inst.Binary)
	fmt.Fprintf(m.out, "Installation method: %s\n", inst.Method)
	output, err := platformStatus()
	if err != nil {
		return err
	}
	fmt.Fprint(m.out, output)
	return nil
}

func (m manager) deactivate(method installationMethod) error {
	inst, err := m.loadInstallation()
	if err != nil {
		return err
	}
	if inst == nil {
		fmt.Fprintln(m.out, "FSClip is already deactivated.")
		return nil
	}
	if inst.Method != method {
		return errors.New("this installation belongs to another installer")
	}
	if err := platformRemoveStartup(inst.Binary); err != nil {
		return err
	}
	if err := removeIfExists(filepath.Join(m.dir, "installation.json")); err != nil {
		return err
	}
	fmt.Fprintln(m.out, "FSClip deactivated. Preferences and watched files were preserved.")
	return nil
}

func (m manager) uninstall() error {
	inst, err := m.loadInstallation()
	if err != nil {
		return err
	}
	if inst == nil {
		fmt.Fprintln(m.out, "No managed installation found; nothing to uninstall.")
		return nil
	}
	if inst.Method != installationMethodScript {
		return errors.New("this installation is managed externally; use its package manager")
	}
	if err := platformRemoveStartup(inst.Binary); err != nil {
		return err
	}
	if inst.PathAdded {
		if err := platformRemovePath(filepath.Dir(inst.Binary)); err != nil {
			return err
		}
	}
	if err := platformDeleteExecutable(inst.Binary); err != nil {
		return err
	}
	if err := removeIfExists(filepath.Join(m.dir, "installation.json")); err != nil {
		return err
	}
	fmt.Fprintln(m.out, "FSClip uninstalled. Watched files, preferences, and logs were preserved.")
	return nil
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func replaceExecutable(source, target string) (undo func() error, commit func(), err error) {
	noop := func() {}
	if samePath(source, target) {
		return func() error { return nil }, noop, nil
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, nil, err
	}
	backup := ""
	if _, err := os.Stat(target); err == nil {
		file, err := os.CreateTemp(filepath.Dir(target), ".fs-clip-backup-*")
		if err != nil {
			return nil, nil, err
		}
		backup = file.Name()
		file.Close()
		os.Remove(backup)
		if err := os.Rename(target, backup); err != nil {
			return nil, nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	undo = func() error {
		if err := removeIfExists(target); err != nil {
			return err
		}
		if backup != "" {
			return os.Rename(backup, target)
		}
		return nil
	}
	if err := atomicWrite(target, data, 0755); err != nil {
		return nil, nil, errors.Join(err, undo())
	}
	return undo, func() {
		if backup != "" {
			os.Remove(backup)
		}
	}, nil
}

func platformCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	preparePlatformCommand(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output), nil
}

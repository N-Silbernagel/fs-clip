//go:build linux || darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func stateDirectory() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "fs-clip"), err
}

func executableName() string { return "fs-clip" }

func defaultBinaryDirectory() (string, error) {
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin"), err
}

func samePath(a, b string) bool {
	left, errLeft := filepath.EvalSymlinks(a)
	right, errRight := filepath.EvalSymlinks(b)
	if errLeft == nil && errRight == nil {
		return left == right
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func acquireManagementLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func platformEnsurePath(string) (bool, error)    { return false, nil }
func platformRemovePath(string) error            { return nil }
func platformDeleteExecutable(path string) error { return removeIfExists(path) }
func preparePlatformCommand(*exec.Cmd)           {}
func prepareCommandLine()                        {}

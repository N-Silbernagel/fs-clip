package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func linuxServicePath() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "systemd", "user", serviceLabel+".service"), err
}

func systemdArgument(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`).Replace(value) + `"`
}

func platformActivate(inst installation, p preferences) error {
	path, err := linuxServicePath()
	if err != nil {
		return err
	}
	unit := fmt.Sprintf("[Unit]\nDescription=FSClip\nStartLimitIntervalSec=0\n[Service]\nType=simple\nRestart=always\nRestartSec=1\nExecStart=%s --watch-dir %s\n\n[Install]\nWantedBy=default.target\n", systemdArgument(inst.Binary), systemdArgument(p.WatchDir))
	if err := atomicWrite(path, []byte(unit), 0644); err != nil {
		return err
	}
	if _, err := platformCommand("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	_, err = platformCommand("systemctl", "--user", "enable", "--now", serviceLabel+".service")
	return err
}

func platformStop(string) error {
	path, err := linuxServicePath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	_, err = platformCommand("systemctl", "--user", "disable", "--now", serviceLabel+".service")
	return err
}

func platformRemoveStartup(binary string) error {
	if err := platformStop(binary); err != nil {
		return err
	}
	path, err := linuxServicePath()
	if err != nil {
		return err
	}
	if err := removeIfExists(path); err != nil {
		return err
	}
	_, err = platformCommand("systemctl", "--user", "daemon-reload")
	return err
}

func platformStatus() (string, error) {
	return platformCommand("systemctl", "--user", "status", "--no-pager", serviceLabel+".service")
}

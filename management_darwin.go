package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func xmlText(value string) string {
	var out strings.Builder
	xml.EscapeText(&out, []byte(value))
	return out.String()
}

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", serviceLabel+".plist"), err
}

func launchAgentDomain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

func platformActivate(inst installation, p preferences) error {
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	logs := filepath.Join(home, "Library", "Logs", "fs-clip")
	if err := os.MkdirAll(logs, 0700); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>--watch-dir</string><string>%s</string></array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>StandardOutPath</key><string>%s</string>
<key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, serviceLabel, xmlText(inst.Binary), xmlText(p.WatchDir), xmlText(filepath.Join(logs, "out.log")), xmlText(filepath.Join(logs, "err.log")))
	if err := atomicWrite(path, []byte(plist), 0644); err != nil {
		return err
	}
	_, err = platformCommand("launchctl", "bootstrap", launchAgentDomain(), path)
	return err
}

func platformStop(string) error {
	target := launchAgentDomain() + "/" + serviceLabel
	if _, err := platformCommand("launchctl", "print", target); err != nil {
		return nil
	}
	_, err := platformCommand("launchctl", "bootout", target)
	return err
}

func platformRemoveStartup(binary string) error {
	if err := platformStop(binary); err != nil {
		return err
	}
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
	return removeIfExists(path)
}

func platformStatus() (string, error) {
	return platformCommand("launchctl", "print", launchAgentDomain()+"/"+serviceLabel)
}

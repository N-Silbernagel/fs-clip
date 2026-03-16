BINDIR := $(HOME)/.local/bin/
# Directory of this Makefile
DIR := $(dir $(realpath $(lastword $(MAKEFILE_LIST))))
LAUNCHAGENTS  := $(HOME)/Library/LaunchAgents/
LOGDIR := $(HOME)/Library/Logs/fs-clip/

COMMAND_LABEL := dev.nils-silbernagel.fs-clip
PLIST := $(COMMAND_LABEL).plist

WATCHDIR ?= ""

.PHONY: build
build:
	go build

.PHONY: symlink
symlink:
	@echo "Symlinking fs-clip to $(BINDIR)"
	@mkdir -p "$(BINDIR)"
	ln -si "$(DIR)fs-clip" "$(BINDIR)fs-clip"

.PHONY: copy-exec
copy-exec:
	@echo "Copying fs-clip to $(BINDIR)"
	@mkdir -p "$(BINDIR)"
	install -m 0755 "$(DIR)fs-clip" "$(BINDIR)fs-clip"

.PHONY: copy-plist
copy-plist:
	@echo "Copying plist to $(LAUNCHAGENTS)"
	@mkdir -p "$(LAUNCHAGENTS)" "$(LOGDIR)"
	sed "s|{USER_HOME}|$(HOME)|g" "$(DIR)$(PLIST)" \
		| sed "s|{WATCH_DIR}|$(WATCHDIR)|g" \
		| sed "s|{BINARY_PATH}|$(BINDIR)fs-clip|g" \
		> "$(LAUNCHAGENTS)$(PLIST)"
	chmod 0644 "$(LAUNCHAGENTS)$(PLIST)"

# Unload existing launchd daemon
.PHONY: unload
unload:
	@echo "Unloading launchd plist"
	@if launchctl list | grep -q "$(COMMAND_LABEL)"; then \
		echo "Unloading current instance of $(PLIST)"; \
		launchctl unload "$(LAUNCHAGENTS)$(PLIST)"; \
	fi

# Load the launchd daemon
.PHONY: load
load:
	@echo "Loading launchd plist"
	launchctl load "$(LAUNCHAGENTS)$(PLIST)"

# Reload the daemon (unload then load)
.PHONY: reload
reload: unload load

# Install target: symlink, reload
.PHONY: install
install: copy-exec copy-plist reload
	@echo "Installation complete."

# Uninstall target: unload daemon and remove symlink
.PHONY: uninstall
uninstall: unload
	@echo "Removing fs-clip from $(BINDIR)"
	@rm -f "$(BINDIR)fs-clip"
	@echo "Removing plist from $(LAUNCHAGENTS)"
	@rm -f "$(LAUNCHAGENTS)$(PLIST)"
	@echo "Uninstallation complete."

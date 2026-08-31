BINARY  := expiry-radar
BIN_DIR := bin
PKG     := ./cmd/expiry-radar

# The editor integrations. Neither is part of the Go build, so every verb that
# touches them is explicit — `make test` stays the Go tests, and nothing here
# silently pulls in a node_modules or a git clone.
VSCODE_DIR := extensions/vscode
NVIM_DIR   := extensions/nvim
# Where Neovim loads a plugin from with no plugin manager involved.
NVIM       ?= nvim
NVIM_SITE  := $(if $(XDG_DATA_HOME),$(XDG_DATA_HOME),$(HOME)/.local/share)/nvim/site
NVIM_LINK  := $(NVIM_SITE)/pack/expiry-radar/start/expiry-radar

.PHONY: all build install uninstall test tidy clean help lint setup \
        ext-build ext-test ext-install ext-uninstall ext-clean

.DEFAULT_GOAL := help

## help: show this help
help:
	@awk '/^## [a-zA-Z0-9_-]+:/ { l=$$0; sub(/^## /,"",l); i=index(l,":"); \
	         printf "  %-14s %s\n", substr(l,1,i-1), substr(l,i+2); next } \
	     /^[a-zA-Z0-9_-]+:.*## / { i=index($$0,":"); j=index($$0,"## "); \
	         printf "  %-14s %s\n", substr($$0,1,i-1), substr($$0,j+3) }' $(MAKEFILE_LIST)

all: build

## build: compile the binary into ./bin
build:
	go build -o $(BIN_DIR)/$(BINARY) $(PKG)

## install: install the binary for the current user (PREFIX=/usr/local for a system path)
# `go install` by default, so it lands wherever this machine's Go is configured
# to put binaries and stays consistent with the `go install ...@latest` in the
# README — the difference being that this installs the working tree, which is
# the whole reason to run it from a checkout.
install:
ifeq ($(strip $(PREFIX)),)
	go install $(PKG)
	@dir="$$(go env GOBIN)"; [ -n "$$dir" ] || dir="$$(go env GOPATH)/bin"; \
	  echo "installed $$dir/$(BINARY)"; \
	  case ":$$PATH:" in \
	    *":$$dir:"*) ;; \
	    *) echo "  note: $$dir is not on your PATH, so \`$(BINARY)\` will not resolve."; \
	       echo "        The editor integrations look there anyway.";; \
	  esac
else
	@$(MAKE) build
	install -d "$(DESTDIR)$(PREFIX)/bin"
	install -m 0755 $(BIN_DIR)/$(BINARY) "$(DESTDIR)$(PREFIX)/bin/$(BINARY)"
	@echo "installed $(DESTDIR)$(PREFIX)/bin/$(BINARY)"
endif

## uninstall: remove the binary installed by `make install`
uninstall:
ifeq ($(strip $(PREFIX)),)
	@dir="$$(go env GOBIN)"; [ -n "$$dir" ] || dir="$$(go env GOPATH)/bin"; \
	  rm -f "$$dir/$(BINARY)" && echo "removed $$dir/$(BINARY)"
else
	rm -f "$(DESTDIR)$(PREFIX)/bin/$(BINARY)"
	@echo "removed $(DESTDIR)$(PREFIX)/bin/$(BINARY)"
endif

## test: run tests
test:
	go test -race -count=1 ./...

## tidy: tidy modules
tidy:
	go mod tidy

## clean: remove build artifacts
# The editor integrations are deliberately not swept up here, for the same
# reason `test` is not `ext-test`: they are separate projects with their own
# toolchains, and `clean` should not cost a `npm ci` to undo.
clean:
	rm -rf $(BIN_DIR)

setup: ## Install the pre-commit hook
	pre-commit install

lint: ## Run all pre-commit checks on the whole tree
	pre-commit run --all-files

## ext-build: package the VS Code extension into a .vsix
ext-build:
	cd $(VSCODE_DIR) && npm ci && npm run package

## ext-test: run both editor integrations' tests
# Depends on build: the contract tests and the smoke test drive the real binary,
# and both skip without one — which would make this pass while the extensions
# had quietly stopped agreeing with the CLI.
ext-test: build
	cd $(VSCODE_DIR) && npm ci && npm run typecheck && npm test
	$(MAKE) -C $(NVIM_DIR) test NVIM=$(NVIM)
	cd $(NVIM_DIR) && $(NVIM) --headless --clean -u tests/smoke.lua

## ext-install: install both editor integrations for the current user
ext-install:
	@command -v code >/dev/null 2>&1 || { \
	  echo "make ext-install: the 'code' command is not on PATH."; \
	  echo "  VS Code ships it under Command Palette -> 'Shell Command: Install code command in PATH'."; \
	  echo "  Skipping the VS Code extension; the Neovim plugin is still installed below."; }
	@if command -v code >/dev/null 2>&1; then \
	  $(MAKE) ext-build && \
	  code --install-extension "$$(ls -t $(VSCODE_DIR)/*.vsix | head -1)" --force && \
	  echo "installed the VS Code extension (reload the window to activate it)"; \
	fi
	@command -v $(NVIM) >/dev/null 2>&1 || { echo "make ext-install: $(NVIM) is not on PATH; skipping the plugin."; exit 0; }
	@mkdir -p "$(dir $(NVIM_LINK))"
	@# A symlink rather than a copy, so `git pull` updates the installed plugin.
	@# Removed first: ln -sfn onto an existing symlink-to-a-directory nests the
	@# new link inside the old target instead of replacing it.
	@rm -rf "$(NVIM_LINK)"
	@ln -s "$(CURDIR)/$(NVIM_DIR)" "$(NVIM_LINK)"
	@$(NVIM) --headless -c "helptags $(NVIM_LINK)/doc" -c qa 2>/dev/null || true
	@echo "linked the Neovim plugin: $(NVIM_LINK) -> $(CURDIR)/$(NVIM_DIR)"
	@echo "  :ExpiryRadarReport to start, :help expiry-radar for the rest."

## ext-uninstall: remove both editor integrations for the current user
ext-uninstall:
	@if command -v code >/dev/null 2>&1; then \
	  code --uninstall-extension fabiocicerchia.expiry-radar || true; \
	fi
	@rm -rf "$(NVIM_LINK)"
	@echo "removed $(NVIM_LINK)"

## ext-clean: remove the editor integrations' build artifacts
ext-clean:
	rm -rf $(VSCODE_DIR)/node_modules $(VSCODE_DIR)/dist $(VSCODE_DIR)/out $(VSCODE_DIR)/*.vsix
	$(MAKE) -C $(NVIM_DIR) clean

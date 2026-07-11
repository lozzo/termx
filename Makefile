.PHONY: help termx-build test-cli test-core test-tui test-cli-v3-smoke test-cli-v3-tmux-smoke test-cli-v3-tmux-terminal-smoke test-cli-v3-tmux-resize-smoke test-cli-v3-tmux-ansi-smoke test-cli-v3-tmux-visual-compare test-cli-v3-tmux-stability-smoke test-cli-default-smoke test-cli-default-deps test-repository

BIN_DIR := $(CURDIR)/bin
TERMX_BIN := $(BIN_DIR)/termx

help:
	@printf '%s\n' \
		'Targets:' \
		'  make termx-build     Build ./bin/termx from cmd/termx' \
		'  make test-cli        Run CLI package tests' \
		'  make test-core       Run core package tests' \
		'  make test-tui        Run TUI package tests' \
		'  make test-cli-v3-smoke Run v3 CLI smoke harness' \
		'  make test-cli-default-smoke Run default CLI daemon smoke' \
		'  make test-cli-default-deps Guard default CLI source against legacy imports' \
		'  make test-cli-v3-tmux-smoke Run optional tmux black-box v3 harness smoke' \
		'  make test-cli-v3-tmux-terminal-smoke Run optional tmux terminal create/attach/input smoke' \
		'  make test-cli-v3-tmux-resize-smoke Run optional tmux resize/layout smoke' \
		'  make test-cli-v3-tmux-ansi-smoke Run optional tmux ANSI/theme/live surface smoke' \
		'  make test-cli-v3-tmux-visual-compare Run optional tmux visual capture and target diff' \
		'  make test-cli-v3-tmux-stability-smoke Run optional tmux short stability smoke' \
		'  make test-repository Test public domains and default CLI smoke'

termx-build:
	mkdir -p "$(BIN_DIR)"
	go build -o "$(TERMX_BIN)" ./cmd/termx

test-cli:
	go test ./cmd/termx -count=1

test-core:
	go test ./core/... -count=1

test-tui:
	go test ./tui/... -count=1

test-cli-v3-smoke:
	set -e; \
	tmp="$$(mktemp -d)"; \
	go build -o "$$tmp/termx" ./cmd/termx; \
	"$$tmp/termx" v3 smoke; \
	"$$tmp/termx" v3 e2e-smoke; \
	rm -rf "$$tmp"

test-cli-v3-tmux-smoke:
	set -e; \
	if ! command -v tmux >/dev/null 2>&1; then \
		echo "tmux not installed; skipping tmux smoke"; \
		exit 0; \
	fi; \
	tmp="$$(mktemp -d)"; \
	go build -o "$$tmp/termx" ./cmd/termx; \
	"$$tmp/termx" v3 tmux-smoke; \
	rm -rf "$$tmp"

test-cli-v3-tmux-terminal-smoke:
	set -e; \
	if ! command -v tmux >/dev/null 2>&1; then \
		echo "tmux not installed; skipping tmux terminal smoke"; \
		exit 0; \
	fi; \
	tmp="$$(mktemp -d)"; \
	go build -o "$$tmp/termx" ./cmd/termx; \
	"$$tmp/termx" v3 tmux-terminal-smoke --termx-bin "$$tmp/termx"; \
	rm -rf "$$tmp"

test-cli-v3-tmux-resize-smoke:
	set -e; \
	if ! command -v tmux >/dev/null 2>&1; then \
		echo "tmux not installed; skipping tmux resize smoke"; \
		exit 0; \
	fi; \
	tmp="$$(mktemp -d)"; \
	go build -o "$$tmp/termx" ./cmd/termx; \
	"$$tmp/termx" v3 tmux-resize-smoke --termx-bin "$$tmp/termx"; \
	rm -rf "$$tmp"

test-cli-v3-tmux-ansi-smoke:
	set -e; \
	if ! command -v tmux >/dev/null 2>&1; then \
		echo "tmux not installed; skipping tmux ansi smoke"; \
		exit 0; \
	fi; \
	tmp="$$(mktemp -d)"; \
	go build -o "$$tmp/termx" ./cmd/termx; \
	"$$tmp/termx" v3 tmux-ansi-smoke --termx-bin "$$tmp/termx"; \
	rm -rf "$$tmp"

test-cli-v3-tmux-visual-compare:
	set -e; \
	if ! command -v tmux >/dev/null 2>&1; then \
		echo "tmux not installed; skipping tmux visual compare"; \
		exit 0; \
	fi; \
	tmp="$$(mktemp -d)"; \
	go build -o "$$tmp/termx" ./cmd/termx; \
	"$$tmp/termx" v3 tmux-visual-compare --termx-bin "$$tmp/termx"; \
	rm -rf "$$tmp"

test-cli-v3-tmux-stability-smoke:
	set -e; \
	if ! command -v tmux >/dev/null 2>&1; then \
		echo "tmux not installed; skipping tmux stability smoke"; \
		exit 0; \
	fi; \
	tmp="$$(mktemp -d)"; \
	go build -o "$$tmp/termx" ./cmd/termx; \
	"$$tmp/termx" v3 tmux-stability-smoke --termx-bin "$$tmp/termx" --rounds 2; \
	rm -rf "$$tmp"

test-cli-default-smoke:
	set -e; \
	tmp="$$(mktemp -d)"; \
	daemon_pid=""; \
	cleanup() { \
		if [ -n "$$daemon_pid" ]; then \
			kill "$$daemon_pid" 2>/dev/null || true; \
			wait "$$daemon_pid" 2>/dev/null || true; \
		fi; \
		rm -rf "$$tmp"; \
	}; \
	trap cleanup EXIT; \
	go build -o "$$tmp/termx" ./cmd/termx; \
	"$$tmp/termx" --help >/dev/null; \
	socket="$$tmp/termx-default.sock"; \
	log="$$tmp/termx-default.log"; \
	"$$tmp/termx" --socket "$$socket" --log-file "$$log" daemon >/dev/null 2>&1 & \
	daemon_pid="$$!"; \
	for _ in $$(seq 1 50); do \
		[ -S "$$socket" ] && break; \
		sleep 0.1; \
	done; \
	if [ ! -S "$$socket" ]; then \
		echo "default core-v2 daemon did not create socket" >&2; \
		exit 1; \
	fi; \
	id="$$("$$tmp/termx" --socket "$$socket" --log-file "$$log" new --name default-smoke -- sleep 30)"; \
	test -n "$$id"; \
	"$$tmp/termx" --socket "$$socket" --log-file "$$log" ls | grep "$$id" >/dev/null; \
	"$$tmp/termx" --socket "$$socket" --log-file "$$log" kill "$$id"; \
	"$$tmp/termx" --socket "$$socket" --log-file "$$log" rm "$$id"; \
	if "$$tmp/termx" --socket "$$socket" --log-file "$$log" ls | grep "$$id" >/dev/null; then \
		echo "removed default smoke terminal is still listed" >&2; \
		exit 1; \
	fi

test-cli-default-deps:
	go test ./cmd/termx -count=1 -run TestDefaultRuntimeSourceDoesNotImportLegacyCoreOrTUI

test-repository: test-core test-tui test-cli-v3-smoke test-cli-default-smoke test-cli-default-deps

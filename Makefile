.PHONY: help localweb-build termx-build remote-daemon remote-dev remote-open remote-status remote-clean remote-hub-both remote-pair test-remote-ui test-termx-cli test-core-v2 test-tui-v3 test-cli-v3-smoke test-cli-v3-tmux-smoke test-cli-v3-tmux-terminal-smoke test-cli-default-smoke test-cli-default-deps test-v2-migration

BIN_DIR := $(CURDIR)/bin
TERMX_BIN := $(BIN_DIR)/termx
LOCAL_WEB_ADDR ?= 127.0.0.1:18888
ICE_TCP_ADDR ?= 127.0.0.1:18889
LOCAL_WEB_ORIGIN := http://$(LOCAL_WEB_ADDR)
REMOTE_UI_DEV_HOST ?= 127.0.0.1
REMOTE_UI_DEV_PORT ?= 5173
REMOTE_UI_DEV_ORIGIN := http://$(REMOTE_UI_DEV_HOST):$(REMOTE_UI_DEV_PORT)
REMOTE_SOCKET ?= $(HOME)/.local/state/termx/termx.sock
REMOTE_LOG ?= $(HOME)/.local/state/termx/termx.log
CONTROL_URL ?= http://114.66.58.243:12306
HUB_URL ?= http://114.66.58.243:8447

help:
	@printf '%s\n' \
		'Targets:' \
		'  make localweb-build  Build remote-ui and sync embedded local web assets' \
		'  make termx-build     Build ./bin/termx from termx-cli/cmd/termx' \
		'  make remote-daemon   Rebuild local web + termx, then run a foreground daemon with local remote env' \
		'  make remote-dev      Build ./bin/termx, ensure local remote is enabled, then start vite dev server' \
		'  make remote-clean    Stop local temp daemons/dev servers and remove known temp sockets' \
		'  make remote-hub-both  Build ./bin/termx and print the command to start one clean both-mode daemon' \
		'  make remote-pair     Build ./bin/termx and generate a termx:// pairing URI from one socket' \
		'  make remote-open     Ensure local remote is enabled, then print the local remote URL' \
		'  make remote-status   Show local remote status through ./bin/termx' \
		'  make test-cli-default-deps Guard default CLI source against legacy imports' \
		'  make test-cli-v3-tmux-smoke Run optional tmux black-box v3 harness smoke' \
		'  make test-cli-v3-tmux-terminal-smoke Run optional tmux terminal create/attach/input smoke' \
		'  make test-v2-migration Test v2/v3 migration modules and default CLI smoke' \
		'' \
		'Variables:' \
		'  LOCAL_WEB_ADDR=<host:port>  default 127.0.0.1:18888' \
		'  ICE_TCP_ADDR=<host:port>    default 127.0.0.1:18889' \
		'  REMOTE_UI_DEV_HOST=<host>   default 127.0.0.1' \
		'  REMOTE_UI_DEV_PORT=<port>   default 5173' \
		'  REMOTE_SOCKET=<path>        default $(HOME)/.local/state/termx/termx.sock' \
		'  CONTROL_URL=<url>           default http://114.66.58.243:12306' \
		'  HUB_URL=<url>               default http://114.66.58.243:8447'

localweb-build:
	cd remote-ui && npm run build:localweb

termx-build:
	mkdir -p "$(BIN_DIR)"
	go build -o "$(TERMX_BIN)" ./termx-cli/cmd/termx

remote-dev: termx-build
	@"$(TERMX_BIN)" remote enable --mode local --addr "$(LOCAL_WEB_ADDR)" --ice-tcp-addr "$(ICE_TCP_ADDR)"
	@printf 'termx local remote: %s\n' "$(LOCAL_WEB_ORIGIN)"
	@printf 'remote-ui dev server: %s\n' "$(REMOTE_UI_DEV_ORIGIN)"
	@printf '%s\n' 'local pair session:'
	@"$(TERMX_BIN)" remote pair
	cd remote-ui && TERMX_LOCAL_WEB_ORIGIN="$(LOCAL_WEB_ORIGIN)" npm run dev -- --host "$(REMOTE_UI_DEV_HOST)" --port "$(REMOTE_UI_DEV_PORT)"

remote-daemon: localweb-build termx-build
	TERMX_REMOTE_LOCAL_WEB_ADDR="$(LOCAL_WEB_ADDR)" TERMX_REMOTE_LOCAL_ICE_TCP_ADDR="$(ICE_TCP_ADDR)" "$(TERMX_BIN)" daemon

remote-open: termx-build
	@"$(TERMX_BIN)" remote enable --mode local --addr "$(LOCAL_WEB_ADDR)" --ice-tcp-addr "$(ICE_TCP_ADDR)"
	@"$(TERMX_BIN)" remote open --print

remote-status: termx-build
	@"$(TERMX_BIN)" remote status

remote-clean:
	@pkill -f '/tmp/termx-wf505' 2>/dev/null || true
	@pkill -f 'termx-501.sock' 2>/dev/null || true
	@pkill -f 'remote-ui/node_modules/.bin/vite --host 127.0.0.1 --port 5173' 2>/dev/null || true
	@pkill -f 'remote-ui/node_modules/.bin/vite --host 127.0.0.1 --port 5174' 2>/dev/null || true
	@pkill -f '/tmp/termx-wf505-chrome-pty' 2>/dev/null || true
	@rm -f /tmp/termx-wf505.sock /var/folders/_k/rv9v4pv16b96_ss090ljksn80000gn/T/termx-501.sock
	@printf '%s\n' 'cleaned known local temp daemons, vite servers, headless chrome, and temp sockets'

remote-hub-both: termx-build
	@printf '%s\n' \
		'Run this in one terminal:' \
		'' \
		'  $(CURDIR)/bin/termx --socket "$(REMOTE_SOCKET)" --log-file "$(REMOTE_LOG)" daemon' \
		'' \
		'Then in another terminal run:' \
		'' \
		'  TERMX_REMOTE_CONTROL_URL="$(CONTROL_URL)" $(CURDIR)/bin/termx --socket "$(REMOTE_SOCKET)" remote enable --mode both --hub-url "$(HUB_URL)"'

remote-pair: termx-build
	@printf '%s\n' \
		'Generate a fresh termx:// pairing URI from one socket with:' \
		'' \
		'  $(CURDIR)/bin/termx --socket "$(REMOTE_SOCKET)" remote pair --uri'

test-remote-ui:
	cd remote-ui && npm test

test-termx-cli:
	go test ./termx-cli/...

test-core-v2:
	cd termx-core-v2 && go test ./... -count=1

test-tui-v3:
	cd termx-tui-v3 && go test ./... -count=1

test-cli-v3-smoke:
	set -e; \
	tmp="$$(mktemp -d)"; \
	go build -o "$$tmp/termx" ./termx-cli/cmd/termx; \
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
	go build -o "$$tmp/termx" ./termx-cli/cmd/termx; \
	"$$tmp/termx" v3 tmux-smoke; \
	rm -rf "$$tmp"

test-cli-v3-tmux-terminal-smoke:
	set -e; \
	if ! command -v tmux >/dev/null 2>&1; then \
		echo "tmux not installed; skipping tmux terminal smoke"; \
		exit 0; \
	fi; \
	tmp="$$(mktemp -d)"; \
	go build -o "$$tmp/termx" ./termx-cli/cmd/termx; \
	"$$tmp/termx" v3 tmux-terminal-smoke --termx-bin "$$tmp/termx"; \
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
	go build -o "$$tmp/termx" ./termx-cli/cmd/termx; \
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
	id="$$("$$tmp/termx" --socket "$$socket" --log-file "$$log" new --name default-smoke -- smoke-shell)"; \
	test -n "$$id"; \
	"$$tmp/termx" --socket "$$socket" --log-file "$$log" ls | grep "$$id" >/dev/null; \
	"$$tmp/termx" --socket "$$socket" --log-file "$$log" kill "$$id"; \
	"$$tmp/termx" --socket "$$socket" --log-file "$$log" rm "$$id"; \
	if "$$tmp/termx" --socket "$$socket" --log-file "$$log" ls | grep "$$id" >/dev/null; then \
		echo "removed default smoke terminal is still listed" >&2; \
		exit 1; \
	fi

test-cli-default-deps:
	cd termx-cli && go test ./cmd/termx -count=1 -run TestDefaultRuntimeSourceDoesNotImportLegacyCoreOrTUI

test-v2-migration: test-core-v2 test-tui-v3 test-cli-v3-smoke test-cli-default-smoke test-cli-default-deps

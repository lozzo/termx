.PHONY: help localweb-build termx-build remote-daemon remote-dev remote-open remote-status test-remote-ui test-termx-cli

BIN_DIR := $(CURDIR)/bin
TERMX_BIN := $(BIN_DIR)/termx
LOCAL_WEB_ADDR ?= 127.0.0.1:18888
ICE_TCP_ADDR ?= 127.0.0.1:18889
LOCAL_WEB_ORIGIN := http://$(LOCAL_WEB_ADDR)
REMOTE_UI_DEV_HOST ?= 127.0.0.1
REMOTE_UI_DEV_PORT ?= 5173
REMOTE_UI_DEV_ORIGIN := http://$(REMOTE_UI_DEV_HOST):$(REMOTE_UI_DEV_PORT)

help:
	@printf '%s\n' \
		'Targets:' \
		'  make localweb-build  Build remote-ui and sync embedded local web assets' \
		'  make termx-build     Build ./bin/termx from termx-cli/cmd/termx' \
		'  make remote-daemon   Rebuild local web + termx, then run a foreground daemon with local remote env' \
		'  make remote-dev      Build ./bin/termx, ensure local remote is enabled, then start vite dev server' \
		'  make remote-open     Ensure local remote is enabled, then print the local remote URL' \
		'  make remote-status   Show local remote status through ./bin/termx' \
		'' \
		'Variables:' \
		'  LOCAL_WEB_ADDR=<host:port>  default 127.0.0.1:18888' \
		'  ICE_TCP_ADDR=<host:port>    default 127.0.0.1:18889' \
		'  REMOTE_UI_DEV_HOST=<host>   default 127.0.0.1' \
		'  REMOTE_UI_DEV_PORT=<port>   default 5173'

localweb-build:
	cd remote-ui && npm run build:localweb

termx-build:
	mkdir -p "$(BIN_DIR)"
	go build -o "$(TERMX_BIN)" ./termx-cli/cmd/termx

remote-dev: termx-build
	@"$(TERMX_BIN)" remote local-only --addr "$(LOCAL_WEB_ADDR)" --ice-tcp-addr "$(ICE_TCP_ADDR)"
	@printf 'termx local remote: %s\n' "$(LOCAL_WEB_ORIGIN)"
	@printf 'remote-ui dev server: %s\n' "$(REMOTE_UI_DEV_ORIGIN)"
	@printf '%s\n' 'local pair session:'
	@"$(TERMX_BIN)" remote pair
	cd remote-ui && TERMX_LOCAL_WEB_ORIGIN="$(LOCAL_WEB_ORIGIN)" npm run dev -- --host "$(REMOTE_UI_DEV_HOST)" --port "$(REMOTE_UI_DEV_PORT)"

remote-daemon: localweb-build termx-build
	TERMX_REMOTE_LOCAL_WEB_ADDR="$(LOCAL_WEB_ADDR)" TERMX_REMOTE_LOCAL_ICE_TCP_ADDR="$(ICE_TCP_ADDR)" "$(TERMX_BIN)" daemon

remote-open: termx-build
	@"$(TERMX_BIN)" remote local-only --addr "$(LOCAL_WEB_ADDR)" --ice-tcp-addr "$(ICE_TCP_ADDR)"
	@"$(TERMX_BIN)" remote open --print

remote-status: termx-build
	@"$(TERMX_BIN)" remote status

test-remote-ui:
	cd remote-ui && npm test

test-termx-cli:
	go test ./termx-cli/...

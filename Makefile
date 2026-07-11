SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

ARTIFACT_DIR := $(CURDIR)/.artifacts
TERMX_BIN := $(ARTIFACT_DIR)/bin/termx
ANDROID_DIR := $(CURDIR)/clients/mobile/android
ANDROID_ARTIFACT_DIR := $(ARTIFACT_DIR)/android
PRIVATE_MODULES := \
	private/cloud/companion \
	private/cloud/control-plane \
	private/cloud/hub \
	private/cloud/relay \
	private/cloud/route-planner \
	private/cloud/web-controller

.PHONY: help build test test-private test-clients test-android test-all doctor clean

help:
	@printf '%s\n' \
		'Targets:' \
		'  make build         Build termx into .artifacts/bin/' \
		'  make test          Test the public Go module' \
		'  make test-private  Test each private cloud Go module when present' \
		'  make test-clients  Generate, test, typecheck, and build both clients' \
		'  make test-android  Build/test Community and optional Official APKs' \
		'  make test-all      Run all repository test gates sequentially' \
		'  make doctor        Check toolchain, generated code, and repository layout' \
		'  make clean         Remove known generated build outputs'

build:
	mkdir -p "$(dir $(TERMX_BIN))"
	GOWORK=off go build -o "$(TERMX_BIN)" ./cmd/termx

test:
	scripts/with-clean-termx-env.sh env GOWORK=off go test ./... -count=1

test-private:
	@if [[ ! -d "$(CURDIR)/private/cloud" ]]; then \
		printf '%s\n' 'private cloud modules are absent; skipping private tests'; \
	else \
		set -e; \
		for module in $(PRIVATE_MODULES); do \
			printf '%s\n' "==> $$module"; \
			(cd "$$module" && "$(CURDIR)/scripts/with-clean-termx-env.sh" env GOWORK=off go test ./... -count=1); \
		done; \
	fi

test-clients:
	node scripts/client-workspace-guard.mjs
	npm run proto
	npm test
	npm run typecheck
	npm run build

test-android:
	npm run cap:build
	mkdir -p "$(ANDROID_ARTIFACT_DIR)"
	cd "$(ANDROID_DIR)" && ./gradlew clean testDebugUnitTest assembleDebug
	cp "$(ANDROID_DIR)/app/build/outputs/apk/debug/app-debug.apk" "$(ANDROID_ARTIFACT_DIR)/community-debug.apk"
	@if [[ -d "$(CURDIR)/private/cloud/mobile/android" ]]; then \
		cd "$(ANDROID_DIR)"; \
		./gradlew -I ../../../private/cloud/mobile/android/official-cloud.init.gradle clean testDebugUnitTest assembleDebug; \
		cp app/build/outputs/apk/debug/app-debug.apk "$(ANDROID_ARTIFACT_DIR)/official-debug.apk"; \
		scripts="$(CURDIR)/scripts/verify-android-apk-boundary.sh"; \
		"$$scripts" "$(ANDROID_ARTIFACT_DIR)/community-debug.apk" "$(ANDROID_ARTIFACT_DIR)/official-debug.apk"; \
	else \
		"$(CURDIR)/scripts/verify-android-apk-boundary.sh" "$(ANDROID_ARTIFACT_DIR)/community-debug.apk"; \
	fi

test-all:
	$(MAKE) test
	$(MAKE) test-private
	$(MAKE) test-clients
	$(MAKE) test-android

doctor:
	scripts/doctor.sh

clean:
	rm -rf "$(ARTIFACT_DIR)" "$(CURDIR)/bin" "$(CURDIR)/.build"
	rm -rf "$(CURDIR)/clients/ui/dist" "$(CURDIR)/clients/mobile/dist"
	find "$(CURDIR)" -maxdepth 1 -type f \( -name '*.test' -o -name '*.cover' -o -name 'cover.out' \) -delete
	find "$(CURDIR)/core" "$(CURDIR)/tui" -type f -name '*.test' -delete
	find "$(ANDROID_DIR)" -type d \( -name build -o -name .gradle \) -prune -exec rm -rf {} +

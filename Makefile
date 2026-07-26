SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := build

ARTIFACT_DIR := $(CURDIR)/.artifacts
MUXVIA_BIN := $(ARTIFACT_DIR)/bin/muxvia
CLOUD_CONTROLLER_BIN := $(ARTIFACT_DIR)/bin/muxvia-cloud-controller
CLOUD_EDGE_BIN := $(ARTIFACT_DIR)/bin/muxvia-cloud-edge
ANDROID_DIR := $(CURDIR)/clients/mobile/android
ANDROID_ARTIFACT_DIR := $(ARTIFACT_DIR)/android

.PHONY: help build build-cloud test test-clients test-android test-all doctor clean

help:
	@printf '%s\n' \
		'Targets:' \
		'  make / make build  Build muxvia into .artifacts/bin/' \
		'  make build-cloud    Build both Muxvia Cloud processes' \
		'  make test          Test the Go module' \
		'  make test-clients  Generate, test, typecheck, and build both clients' \
		'  make test-android  Build/test the Muxvia Android APK' \
		'  make test-all      Run all repository test gates sequentially' \
		'  make doctor        Check toolchain, generated code, and repository layout' \
		'  make clean         Remove known generated build outputs'

build:
	mkdir -p "$(dir $(MUXVIA_BIN))"
	GOWORK=off go build -o "$(MUXVIA_BIN)" ./cmd/muxvia

build-cloud:
	mkdir -p "$(dir $(CLOUD_CONTROLLER_BIN))"
	GOWORK=off go build -o "$(CLOUD_CONTROLLER_BIN)" ./cmd/muxvia-cloud-controller
	GOWORK=off go build -o "$(CLOUD_EDGE_BIN)" ./cmd/muxvia-cloud-edge

test:
	scripts/with-clean-muxvia-env.sh env GOWORK=off go test ./... -count=1

test-clients:
	node scripts/client-workspace-guard.mjs
	npm run proto
	npm run test:i18n
	npm test
	npm run typecheck
	npm run build

test-android:
	npm run cap:build
	mkdir -p "$(ANDROID_ARTIFACT_DIR)"
	cd "$(ANDROID_DIR)" && ./gradlew clean testDebugUnitTest assembleDebug
	cp "$(ANDROID_DIR)/app/build/outputs/apk/debug/app-debug.apk" "$(ANDROID_ARTIFACT_DIR)/app-debug.apk"
	scripts/verify-android-apk-boundary.sh "$(ANDROID_ARTIFACT_DIR)/app-debug.apk"

test-all:
	$(MAKE) test
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

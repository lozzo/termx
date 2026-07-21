SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

ARTIFACT_DIR := $(CURDIR)/.artifacts
MUXVIA_BIN := $(ARTIFACT_DIR)/bin/muxvia
ANDROID_DIR := $(CURDIR)/clients/mobile/android
ANDROID_ARTIFACT_DIR := $(ARTIFACT_DIR)/android
PRIVATE_MODULES := \
	private/cloud/companion \
	private/cloud/control-plane \
	private/cloud/controller \
	private/cloud/devcloud \
	private/cloud/edge \
	private/cloud/hub \
	private/cloud/relay \
	private/cloud/route-planner \
	private/cloud/web-controller

.PHONY: help build build-cloud-test build-web-controller build-web-controller-linux cloud-dev test test-private test-cloud-controller-edge test-clients test-android test-all doctor clean

help:
	@printf '%s\n' \
		'Targets:' \
		'  make build         Build muxvia into .artifacts/bin/' \
		'  make build-cloud-test  Build self-contained muxvia + staging Companion test suite' \
		'  make build-web-controller  Build the React static Web Controller' \
		'  make build-web-controller-linux  Alias for the platform-independent static Web Controller build' \
		'  make cloud-dev     Start the explicit single-region dev cloud' \
		'  make test          Test the public Go module' \
		'  make test-private  Test each private cloud Go module when present' \
		'  make test-cloud-controller-edge  Test one Controller plus two Edge processes' \
		'  make test-clients  Generate, test, typecheck, and build both clients' \
		'  make test-android  Build/test the standard and dev-cloud Muxvia APK' \
		'  make test-all      Run all repository test gates sequentially' \
		'  make doctor        Check toolchain, generated code, and repository layout' \
		'  make clean         Remove known generated build outputs'

build:
	mkdir -p "$(dir $(MUXVIA_BIN))"
	GOWORK=off go build -o "$(MUXVIA_BIN)" ./cmd/muxvia

build-cloud-test:
	scripts/build_cloud_test.sh

build-web-controller:
	rm -rf "$(ARTIFACT_DIR)/web-controller"
	mkdir -p "$(ARTIFACT_DIR)/web-controller/dist" "$(ARTIFACT_DIR)/web-controller/config"
	npm run build --workspace @muxvia/web-controller
	cp -R private/cloud/web-controller/web/dist/. "$(ARTIFACT_DIR)/web-controller/dist/"
	cp private/cloud/web-controller/config/plans.json "$(ARTIFACT_DIR)/web-controller/config/plans.json"

build-web-controller-linux:
	$(MAKE) build-web-controller GOOS=linux GOARCH=amd64 CGO_ENABLED=0

cloud-dev:
	mkdir -p "$(ARTIFACT_DIR)/cloud-dev"
	scripts/with-test-postgres.sh go run ./private/cloud/devcloud/cmd/muxvia-cloud-dev --manifest "$(ARTIFACT_DIR)/cloud-dev/runtime.json"

test:
	scripts/with-clean-muxvia-env.sh env GOWORK=off go test ./... -count=1

test-private:
	@if [[ ! -d "$(CURDIR)/private/cloud" ]]; then \
		printf '%s\n' 'private cloud modules are absent; skipping private tests'; \
	else \
		set -e; \
		for module in $(PRIVATE_MODULES); do \
			printf '%s\n' "==> $$module"; \
			(cd "$$module" && "$(CURDIR)/scripts/with-clean-muxvia-env.sh" env GOWORK=off "$(CURDIR)/scripts/with-test-postgres.sh" go test ./... -count=1); \
		done; \
	fi

test-cloud-controller-edge:
	cd private/cloud/devcloud && "$(CURDIR)/scripts/with-clean-muxvia-env.sh" env GOWORK=off "$(CURDIR)/scripts/with-test-postgres.sh" go test ./cmd/muxvia-cloud-dev -run TestSupervisorStartsControllerAndTwoIndependentEdges -count=1

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
	cp "$(ANDROID_DIR)/app/build/outputs/apk/debug/app-debug.apk" "$(ANDROID_ARTIFACT_DIR)/app-debug.apk"
	cd "$(ANDROID_DIR)" && ./gradlew -PmuxviaDevCloud=true clean testDebugUnitTest assembleDebug
	cp "$(ANDROID_DIR)/app/build/outputs/apk/debug/app-debug.apk" "$(ANDROID_ARTIFACT_DIR)/app-devcloud-debug.apk"
	scripts/verify-android-apk-boundary.sh "$(ANDROID_ARTIFACT_DIR)/app-debug.apk" "$(ANDROID_ARTIFACT_DIR)/app-devcloud-debug.apk"

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
	rm -rf "$(CURDIR)/private/cloud/web-controller/web/dist"
	find "$(CURDIR)" -maxdepth 1 -type f \( -name '*.test' -o -name '*.cover' -o -name 'cover.out' \) -delete
	find "$(CURDIR)/core" "$(CURDIR)/tui" -type f -name '*.test' -delete
	find "$(ANDROID_DIR)" -type d \( -name build -o -name .gradle \) -prune -exec rm -rf {} +

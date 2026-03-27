SHELL := /bin/zsh

BUILD_DIR := build
BACKEND_BUILD_DIR := $(BUILD_DIR)/backend
BACKEND_BINARY := $(BACKEND_BUILD_DIR)/meeting
WEB_BUILD_DIR := $(BUILD_DIR)/frontend
GO_CACHE_DIR := $(BUILD_DIR)/.cache/go-build
GO_TMP_DIR := $(BUILD_DIR)/.tmp/go
RUN_DIR := $(BUILD_DIR)/run
RUN_LOG_DIR := $(RUN_DIR)/logs
RUN_DATA_DIR := $(RUN_DIR)/data

.PHONY: build build-backend build-frontend run run-backend run-frontend clean

build: build-backend build-frontend

build-backend:
	@mkdir -p "$(BACKEND_BUILD_DIR)" "$(GO_CACHE_DIR)" "$(GO_TMP_DIR)"
	GOCACHE="$(abspath $(GO_CACHE_DIR))" \
	GOTMPDIR="$(abspath $(GO_TMP_DIR))" \
	go build -o "$(abspath $(BACKEND_BINARY))" ./cmd/server

build-frontend:
	@mkdir -p "$(WEB_BUILD_DIR)" "$(BUILD_DIR)/.cache/typescript" "$(BUILD_DIR)/.cache/vite"
	cd web && npm run build

run:
	@echo "请使用 make run-backend 或 make run-frontend"

run-backend:
	@mkdir -p "$(RUN_LOG_DIR)" "$(RUN_DATA_DIR)" "$(GO_CACHE_DIR)" "$(GO_TMP_DIR)"
	MEETING_LOG_DIR="$(abspath $(RUN_LOG_DIR))" \
	MEETING_SQLITE_PATH="$(abspath $(RUN_DATA_DIR))/meeting.db" \
	GOCACHE="$(abspath $(GO_CACHE_DIR))" \
	GOTMPDIR="$(abspath $(GO_TMP_DIR))" \
	go run ./cmd/server

run-frontend:
	@mkdir -p "$(BUILD_DIR)/.cache/typescript" "$(BUILD_DIR)/.cache/vite"
	cd web && npm run dev -- --host 0.0.0.0

clean:
	rm -rf "$(BUILD_DIR)"

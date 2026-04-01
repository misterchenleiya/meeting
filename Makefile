SHELL := /bin/zsh

PROJECT_NAME := meeting
BUILD_DIR := build
BACKEND_BUILD_DIR := $(BUILD_DIR)/backend
BACKEND_BINARY := $(BACKEND_BUILD_DIR)/meeting
WEB_BUILD_DIR := $(BUILD_DIR)/frontend
GO_CACHE_DIR := $(BUILD_DIR)/.cache/go-build
GO_TMP_DIR := $(BUILD_DIR)/.tmp/go
RELEASE_ROOT := $(BUILD_DIR)/release/$(PROJECT_NAME)
RELEASE_BACKEND_DIR := $(RELEASE_ROOT)/backend
RELEASE_FRONTEND_DIR := $(RELEASE_ROOT)/frontend
RELEASE_COTURN_DIR := $(RELEASE_ROOT)/coturn
ARCHIVE_FILE := $(PROJECT_NAME)_$(shell git rev-parse --short=12 HEAD).tar.gz
ARCHIVE_PATH := $(BUILD_DIR)/$(ARCHIVE_FILE)
LATEST_FILE := $(BUILD_DIR)/latest.txt
CURRENT_FILE := $(RELEASE_ROOT)/current.txt
UPLOAD_BASE := https://upload.07c2.com
UPLOAD_PATH := /$(PROJECT_NAME)
UPLOAD_TIMEOUT_SECONDS ?= 30
UPLOAD_MAX_RETRIES ?= 5
UPLOAD_RETRY_DELAY_SECONDS ?= 1
FRONTEND_API_BASE_URL := https://api.07c2.com.cn/meeting/
FRONTEND_SIGNAL_BASE_URL := https://api.07c2.com.cn/meeting/
FRONTEND_STUN_URLS := stun:stun.l.google.com:19302
TURN_HOST := turn.meeting.07c2.com.cn
TURN_USERNAME := meeting
TURN_CREDENTIAL ?= CHANGE_ME_STRONG_PASSWORD
COTURN_LISTEN_PORT ?= 3478
COTURN_TLS_LISTEN_PORT ?= 5349
COTURN_MIN_PORT ?= 52000
COTURN_MAX_PORT ?= 52048
FRONTEND_TURN_URLS := turn:$(TURN_HOST):$(COTURN_LISTEN_PORT)?transport=udp,turn:$(TURN_HOST):$(COTURN_LISTEN_PORT)?transport=tcp,turns:$(TURN_HOST):$(COTURN_TLS_LISTEN_PORT)?transport=tcp

.PHONY: build build-backend build-frontend build-backend-linux build-frontend-release linux stage-release pack upload publish clean

build: build-backend build-frontend

build-backend:
	@mkdir -p "$(BACKEND_BUILD_DIR)" "$(GO_CACHE_DIR)" "$(GO_TMP_DIR)"
	GOCACHE="$(abspath $(GO_CACHE_DIR))" \
	GOTMPDIR="$(abspath $(GO_TMP_DIR))" \
	go build -o "$(abspath $(BACKEND_BINARY))" ./cmd/server

build-frontend:
	@mkdir -p "$(WEB_BUILD_DIR)" "$(BUILD_DIR)/.cache/typescript" "$(BUILD_DIR)/.cache/vite"
	cd web && npm run build

build-backend-linux:
	@mkdir -p "$(BACKEND_BUILD_DIR)" "$(GO_CACHE_DIR)" "$(GO_TMP_DIR)"
	@command -v docker >/dev/null 2>&1 || { echo "docker is required for make linux" >&2; exit 1; }
	docker run --rm --platform linux/amd64 \
		--user "$$(id -u):$$(id -g)" \
		-v "$(CURDIR):/workspace" \
		-w /workspace \
		-e GOCACHE=/workspace/$(GO_CACHE_DIR) \
		-e GOTMPDIR=/workspace/$(GO_TMP_DIR) \
		-e CGO_ENABLED=1 \
		-e GOOS=linux \
		-e GOARCH=amd64 \
		golang:1.24-bookworm \
		/usr/local/go/bin/go build -trimpath -o build/backend/meeting ./cmd/server

build-frontend-release:
	@mkdir -p "$(WEB_BUILD_DIR)" "$(BUILD_DIR)/.cache/typescript" "$(BUILD_DIR)/.cache/vite"
	cd web && \
		VITE_MEETING_API_BASE_URL="$(FRONTEND_API_BASE_URL)" \
		VITE_MEETING_SIGNALING_BASE_URL="$(FRONTEND_SIGNAL_BASE_URL)" \
		VITE_MEETING_STUN_URLS="$(FRONTEND_STUN_URLS)" \
		VITE_MEETING_TURN_URLS="$(FRONTEND_TURN_URLS)" \
		VITE_MEETING_TURN_USERNAME="$(TURN_USERNAME)" \
		VITE_MEETING_TURN_CREDENTIAL="$(TURN_CREDENTIAL)" \
		npm run build

linux: build-backend-linux build-frontend-release

stage-release: linux
	@set -euo pipefail; \
	rm -rf "$(RELEASE_ROOT)"; \
	mkdir -p "$(RELEASE_BACKEND_DIR)" "$(RELEASE_FRONTEND_DIR)/dist" "$(RELEASE_COTURN_DIR)"; \
	[ -x "$(BACKEND_BINARY)" ] || { echo "backend binary missing or not executable: $(BACKEND_BINARY)" >&2; exit 1; }; \
	[ -f "$(WEB_BUILD_DIR)/index.html" ] || { echo "frontend build output missing: $(WEB_BUILD_DIR)/index.html" >&2; exit 1; }; \
	[ -f "docker-compose.yml" ] || { echo "docker-compose.yml missing" >&2; exit 1; }; \
	[ -f "deploy/frontend/nginx.conf" ] || { echo "deploy/frontend/nginx.conf missing" >&2; exit 1; }; \
	[ -f "deploy/coturn/turnserver.conf" ] || { echo "deploy/coturn/turnserver.conf missing" >&2; exit 1; }; \
	command -v python3 >/dev/null 2>&1 || { echo "python3 is required to render coturn config" >&2; exit 1; }; \
	cp "$(BACKEND_BINARY)" "$(RELEASE_BACKEND_DIR)/meeting"; \
	chmod 755 "$(RELEASE_BACKEND_DIR)/meeting"; \
	cp -R "$(WEB_BUILD_DIR)/." "$(RELEASE_FRONTEND_DIR)/dist/"; \
	cp "deploy/frontend/nginx.conf" "$(RELEASE_FRONTEND_DIR)/nginx.conf"; \
	TURN_HOST="$(TURN_HOST)" TURN_USERNAME="$(TURN_USERNAME)" TURN_CREDENTIAL="$(TURN_CREDENTIAL)" COTURN_LISTEN_PORT="$(COTURN_LISTEN_PORT)" COTURN_TLS_LISTEN_PORT="$(COTURN_TLS_LISTEN_PORT)" COTURN_MIN_PORT="$(COTURN_MIN_PORT)" COTURN_MAX_PORT="$(COTURN_MAX_PORT)" python3 -c 'from pathlib import Path; import os; src = Path("deploy/coturn/turnserver.conf").read_text(); host = os.environ["TURN_HOST"]; user = os.environ["TURN_USERNAME"]; credential = os.environ["TURN_CREDENTIAL"]; listen_port = os.environ["COTURN_LISTEN_PORT"]; tls_port = os.environ["COTURN_TLS_LISTEN_PORT"]; min_port = os.environ["COTURN_MIN_PORT"]; max_port = os.environ["COTURN_MAX_PORT"]; src = src.replace("listening-port=3478", "listening-port=" + listen_port); src = src.replace("tls-listening-port=5349", "tls-listening-port=" + tls_port); src = src.replace("realm=turn.meeting.07c2.com.cn", "realm=" + host); src = src.replace("server-name=turn.meeting.07c2.com.cn", "server-name=" + host); src = src.replace("cert=/etc/letsencrypt/live/turn.meeting.07c2.com.cn/fullchain.pem", "cert=/etc/letsencrypt/live/" + host + "/fullchain.pem"); src = src.replace("pkey=/etc/letsencrypt/live/turn.meeting.07c2.com.cn/privkey.pem", "pkey=/etc/letsencrypt/live/" + host + "/privkey.pem"); src = src.replace("user=meeting:CHANGE_ME_STRONG_PASSWORD", "user=" + user + ":" + credential); src = src.replace("min-port=52000", "min-port=" + min_port); src = src.replace("max-port=52048", "max-port=" + max_port); Path("$(RELEASE_COTURN_DIR)/turnserver.conf").write_text(src)'; \
	COTURN_LISTEN_PORT="$(COTURN_LISTEN_PORT)" COTURN_TLS_LISTEN_PORT="$(COTURN_TLS_LISTEN_PORT)" COTURN_MIN_PORT="$(COTURN_MIN_PORT)" COTURN_MAX_PORT="$(COTURN_MAX_PORT)" python3 -c 'from pathlib import Path; import os; src = Path("docker-compose.yml").read_text(); listen_port = os.environ["COTURN_LISTEN_PORT"]; tls_port = os.environ["COTURN_TLS_LISTEN_PORT"]; min_port = os.environ["COTURN_MIN_PORT"]; max_port = os.environ["COTURN_MAX_PORT"]; src = src.replace("$${COTURN_LISTEN_PORT:-3478}", listen_port); src = src.replace("$${COTURN_TLS_LISTEN_PORT:-5349}", tls_port); src = src.replace("$${COTURN_MIN_PORT:-52000}", min_port); src = src.replace("$${COTURN_MAX_PORT:-52048}", max_port); Path("$(RELEASE_ROOT)/docker-compose.yml").write_text(src)'; \
	cp scripts/*.sh "$(RELEASE_ROOT)/"; \
	cp scripts/env.example "$(RELEASE_ROOT)/env.example"; \
	chmod 755 "$(RELEASE_ROOT)"/*.sh; \
	printf '%s\n' "$(ARCHIVE_FILE)" > "$(CURRENT_FILE)"

pack: stage-release
	@set -euo pipefail; \
	rm -f "$(ARCHIVE_PATH)" "$(LATEST_FILE)"; \
	tar -C "$(RELEASE_ROOT)" -czf "$(ARCHIVE_PATH)" .; \
	if command -v sha256sum >/dev/null 2>&1; then \
		sha256="$$(sha256sum "$(ARCHIVE_PATH)" | awk '{print $$1}')"; \
	elif command -v shasum >/dev/null 2>&1; then \
		sha256="$$(shasum -a 256 "$(ARCHIVE_PATH)" | awk '{print $$1}')"; \
	else \
		echo "no sha256 tool found" >&2; \
		exit 1; \
	fi; \
		printf 'filename: %s\nsha256sum: %s\n' "$(ARCHIVE_FILE)" "$$sha256" > "$(LATEST_FILE)"; \
		printf 'packed %s\n' "$(ARCHIVE_PATH)"

upload:
	@set -euo pipefail; \
	[ -f "$(LATEST_FILE)" ] || { echo "$(LATEST_FILE) not found" >&2; exit 1; }; \
	artifact_name="$$(sed -n 's/^filename:[[:space:]]*//p' "$(LATEST_FILE)" | head -n1 | tr -d '\r')"; \
	sha_expected="$$(sed -n 's/^sha256sum:[[:space:]]*//p' "$(LATEST_FILE)" | head -n1 | tr -d '\r')"; \
	[[ -n "$$artifact_name" ]] || { echo "filename is missing in $(LATEST_FILE)" >&2; exit 1; }; \
	[[ -n "$$sha_expected" ]] || { echo "sha256sum is missing in $(LATEST_FILE)" >&2; exit 1; }; \
	artifact_path="$(BUILD_DIR)/$$artifact_name"; \
	[ -f "$$artifact_path" ] || { echo "$$artifact_path not found" >&2; exit 1; }; \
	if command -v sha256sum >/dev/null 2>&1; then \
		sha_actual="$$(sha256sum "$$artifact_path" | awk '{print $$1}')"; \
	elif command -v shasum >/dev/null 2>&1; then \
		sha_actual="$$(shasum -a 256 "$$artifact_path" | awk '{print $$1}')"; \
	else \
		echo "no sha256 tool found" >&2; \
		exit 1; \
	fi; \
	[[ "$$sha_actual" == "$$sha_expected" ]] || { echo "sha256 mismatch for $$artifact_path" >&2; echo "expected=$$sha_expected actual=$$sha_actual" >&2; exit 1; }; \
	if [[ -z "$${deploy_username:-}" ]]; then \
		printf 'deploy username: '; \
		IFS= read -r deploy_username; \
	fi; \
	if [[ -z "$${deploy_username:-}" ]]; then \
		echo "deploy username is empty" >&2; \
		exit 1; \
	fi; \
	if [[ -z "$${deploy_password:-}" ]]; then \
		printf 'deploy password: '; \
		stty -echo; \
		IFS= read -r deploy_password; \
		stty echo; \
		printf '\n'; \
	fi; \
	if [[ -z "$${deploy_password:-}" ]]; then \
		echo "deploy password is empty" >&2; \
		exit 1; \
	fi; \
	export deploy_username deploy_password; \
	UPLOAD_BASE="$(UPLOAD_BASE)" \
	UPLOAD_TIMEOUT_SECONDS="$(UPLOAD_TIMEOUT_SECONDS)" \
	UPLOAD_MAX_RETRIES="$(UPLOAD_MAX_RETRIES)" \
	UPLOAD_RETRY_DELAY_SECONDS="$(UPLOAD_RETRY_DELAY_SECONDS)" \
	./scripts/upload.sh "$$artifact_path" "$(UPLOAD_PATH)"; \
	UPLOAD_BASE="$(UPLOAD_BASE)" \
	UPLOAD_TIMEOUT_SECONDS="$(UPLOAD_TIMEOUT_SECONDS)" \
	UPLOAD_MAX_RETRIES="$(UPLOAD_MAX_RETRIES)" \
	UPLOAD_RETRY_DELAY_SECONDS="$(UPLOAD_RETRY_DELAY_SECONDS)" \
	./scripts/upload.sh "$(LATEST_FILE)" "$(UPLOAD_PATH)"

publish:
	@set -euo pipefail; \
	if [[ -z "$${deploy_username:-}" ]]; then \
		printf 'deploy username: '; \
		IFS= read -r deploy_username; \
	fi; \
	if [[ -z "$${deploy_username:-}" ]]; then \
		echo "deploy username is empty" >&2; \
		exit 1; \
	fi; \
	if [[ -z "$${deploy_password:-}" ]]; then \
		printf 'deploy password: '; \
		stty -echo; \
		IFS= read -r deploy_password; \
		stty echo; \
		printf '\n'; \
	fi; \
	if [[ -z "$${deploy_password:-}" ]]; then \
		echo "deploy password is empty" >&2; \
		exit 1; \
	fi; \
	export deploy_username deploy_password; \
	$(MAKE) --no-print-directory clean && \
	$(MAKE) --no-print-directory linux && \
	$(MAKE) --no-print-directory pack && \
	$(MAKE) --no-print-directory upload

clean:
	rm -rf "$(BUILD_DIR)"

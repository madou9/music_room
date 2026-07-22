# Music Room - developer command surface.
#
# Run from the repo root. Covers the full stack (Docker), the Go server, and the
# Flutter mobile/web app, so no manual setup is needed to run or test. Every
# target prints the URLs, ports, and next steps it is relevant to.
#
# The server-internal targets still live in server/Makefile; this file is the
# single entry point for day-to-day work.

SHELL := /bin/bash

-include .env

# Host-side service endpoints (the server maps container 8080 -> host 8081).
# Set HOSTIP in .env to your machine's IP if running on a remote/WSL host.
HOSTIP      ?= localhost
export HOSTIP

# Tooling. Override on the command line if your paths differ, e.g.
#   make mobile DEVICE=ABC123 ADB=/opt/android/platform-tools/adb
FLUTTER      ?= flutter
ANDROID_HOME ?= $(HOME)/Android/Sdk
ADB          ?= $(shell command -v adb 2>/dev/null || echo $(ANDROID_HOME)/platform-tools/adb)
DEVICE       ?= $(shell $(ADB) get-serialno 2>/dev/null)
WEB_PORT     ?= 5000
APP_ID       ?= com.musicroom.music_room
DC           := docker compose
API_URL     := http://$(HOSTIP):8081
HEALTH_URL  := http://$(HOSTIP):8081/health
SWAGGER_URL := http://$(HOSTIP):8081/api/v1/docs/index.html
MAILPIT_URL := http://$(HOSTIP):8025
DB_HOST     := $(HOSTIP)
DB_PORT     := 5437

APK_DEBUG   := build/app/outputs/flutter-apk/app-debug.apk
APK_RELEASE := build/app/outputs/flutter-apk/app-release.apk

.DEFAULT_GOAL := help

.PHONY: help up down reset server re-server logs rebuild \
        mobile re-mobile web re-web apk apk-release install devices \
        test test-server test-mobile lint \
        migrate migrate-down seed docs deps clean info \
        load-test _load-seed _load-run \
        _ensure-device _adb-reverse _wait-server

## ---- Help -----------------------------------------------------------------

help: ## Show this help
	@echo "Music Room - make targets:"
	@echo ""
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Override tooling, e.g.  make mobile DEVICE=ABC123  |  make web WEB_PORT=5050"

## ---- Full stack (Docker) --------------------------------------------------

up: ## Run everything: build + start the stack, migrate, seed, print URLs
	@echo ">> Starting Postgres + Mailpit + server (Docker)..."
	$(DC) up --build -d --wait
	@$(MAKE) --no-print-directory migrate
	@$(MAKE) --no-print-directory seed
	@$(MAKE) --no-print-directory _wait-server
	@$(MAKE) --no-print-directory info

server: ## Run just the server + its dependencies (detached), then migrate
	$(DC) up --build -d --wait
	@$(MAKE) --no-print-directory migrate
	@$(MAKE) --no-print-directory _wait-server
	@$(MAKE) --no-print-directory info

re-server: ## Re-run the server container (Air also live-reloads on save)
	$(DC) restart server
	@echo ">> Server restarted. API at $(API_URL) | logs: make logs"

logs: ## Follow the server logs
	$(DC) logs server -f

rebuild: ## Rebuild the server image from scratch (no cache)
	$(DC) build --no-cache server
	@echo ">> Server image rebuilt. Run 'make up' to start."

down: ## Stop the stack, keep the database volume
	$(DC) down
	@echo ">> Stack stopped, data kept. 'make up' to start, 'make reset' to wipe the DB."

reset: ## Stop the stack and WIPE the database volume (destructive; FORCE=1 to skip the prompt)
	@echo "!! WARNING: this deletes all Postgres data."
	@[ "$(FORCE)" = "1" ] || { read -r -p "Type 'y' to confirm: " ans && [ "$$ans" = "y" ] || { echo "Aborted."; exit 1; }; }
	$(DC) down -v
	@echo ">> Volumes removed. The next 'make up' starts from an empty, freshly migrated DB."

## ---- Mobile (Android device) ----------------------------------------------

mobile: ## Run the mobile app in debug on a device (pub get + adb reverse + run)
	@$(MAKE) --no-print-directory _ensure-device
	$(FLUTTER) pub get
	@$(MAKE) --no-print-directory _adb-reverse
	@echo ">> Launching debug build on $(DEVICE). The app reaches $(API_URL) via adb reverse."
	@echo ">> Press 'r' to hot reload, 'R' to hot restart, 'q' to quit."
	$(FLUTTER) run -d $(DEVICE)

re-mobile: ## Re-run the mobile app in debug (skip pub get, reuse adb reverse)
	@$(MAKE) --no-print-directory _ensure-device
	@$(MAKE) --no-print-directory _adb-reverse
	@echo ">> Relaunching on $(DEVICE)..."
	$(FLUTTER) run -d $(DEVICE)

apk: ## Build a debug APK and print its path
	$(FLUTTER) build apk --debug
	@echo ">> APK: $(APK_DEBUG)"
	@echo ">> Install + launch on a device: make install"

apk-release: ## Build a release APK and print its path
	$(FLUTTER) build apk --release
	@echo ">> APK: $(APK_RELEASE)"

install: ## Install the latest debug APK on the device and launch it
	@$(MAKE) --no-print-directory _ensure-device
	@test -f $(APK_DEBUG) || { echo "!! $(APK_DEBUG) not found. Run 'make apk' first."; exit 1; }
	$(ADB) -s $(DEVICE) install -r $(APK_DEBUG)
	$(ADB) -s $(DEVICE) shell monkey -p $(APP_ID) -c android.intent.category.LAUNCHER 1 >/dev/null
	@echo ">> Installed and launched $(APP_ID) on $(DEVICE)."

devices: ## List connected Android devices (adb)
	@$(ADB) devices -l 2>/dev/null || echo "!! adb not found at '$(ADB)'. Set ADB=/path/to/adb or install platform-tools."

## ---- Web (browser) --------------------------------------------------------

web: ## Run the web app in debug (headless web-server on WEB_PORT)
	$(FLUTTER) pub get
	@echo ">> Serving web debug at http://localhost:$(WEB_PORT)"
	@echo "!! The server must allow this origin. Set ALLOWED_ORIGINS=http://localhost:$(WEB_PORT) in server/.env, then 'make re-server'."
	$(FLUTTER) run -d web-server --web-port $(WEB_PORT)

re-web: ## Re-run the web app in debug (skip pub get)
	@echo ">> Serving web debug at http://localhost:$(WEB_PORT) (ensure ALLOWED_ORIGINS includes it)"
	$(FLUTTER) run -d web-server --web-port $(WEB_PORT)

## ---- Tests & lint ---------------------------------------------------------

test: ## Run all tests + linters (server + mobile), CI parity
	@$(MAKE) --no-print-directory test-server
	@$(MAKE) --no-print-directory test-mobile

test-server: ## Server: go test + go vet + staticcheck
	cd server && go test ./... && go vet ./...
	cd server && (command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || go run honnef.co/go/tools/cmd/staticcheck@latest ./...)
	@echo ">> Server tests + vet + staticcheck passed."

test-mobile: ## Mobile: flutter analyze + flutter test
	$(FLUTTER) analyze
	$(FLUTTER) test
	@echo ">> Mobile analyze + tests passed."

lint: ## Linters only (go vet + staticcheck + flutter analyze)
	cd server && go vet ./...
	cd server && (command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || go run honnef.co/go/tools/cmd/staticcheck@latest ./...)
	$(FLUTTER) analyze
	@echo ">> Lint clean."

## ---- Database, docs, deps -------------------------------------------------

migrate: ## Apply database migrations (against the Docker Postgres)
	$(DC) run --rm server go run ./cmd/migrate/main.go up
	@echo ">> Migrations applied."

migrate-down: ## Roll back all migrations (down)
	$(DC) run --rm server go run ./cmd/migrate/main.go down
	@echo ">> Migrations rolled back."

seed: ## Seed the database with the test user (idempotent)
	$(DC) run --rm server go run ./cmd/seed/main.go
	@echo ">> Seed complete. Test user: test@example.com / password123"

docs: ## Regenerate the Swagger docs from handler annotations
	cd server && go run github.com/swaggo/swag/cmd/swag@v1.8.12 init -g cmd/main.go -o docs --parseDependency --parseInternal
	@echo ">> Swagger written to server/docs. UI at $(SWAGGER_URL) when the server is up."

deps: ## Fetch Flutter and Go dependencies
	$(FLUTTER) pub get
	cd server && go mod download
	@echo ">> Dependencies fetched."

clean: ## Remove Flutter and Go build artifacts
	$(FLUTTER) clean
	rm -rf server/bin
	@# server/tmp is written by Air inside the container (root-owned), so remove it from there
	-$(DC) run --rm --no-deps -T server rm -rf tmp
	@echo ">> Build artifacts cleaned."

info: ## Print the running services, URLs and ports
	@echo ""
	@echo "  Music Room is up:"
	@echo "    API .......... $(API_URL)      (health: $(HEALTH_URL))"
	@echo "    Swagger UI ... $(SWAGGER_URL)"
	@echo "    Mailpit ...... $(MAILPIT_URL)    (SMTP on 1025)"
	@echo "    Postgres ..... $(DB_HOST):$(DB_PORT)  (db: musicroom, user: postgres)"
	@echo ""
	@echo "  Next: 'make mobile' (device) | 'make web' (browser) | 'make logs' | 'make down'"
	@echo ""

## ---- Load tests -----------------------------------------------------------

K6 := $(shell command -v k6 2>/dev/null || echo ~/.local/bin/k6)

load-test: _load-seed _load-run ## Seed DB + run all 3 k6 load-test scripts sequentially

_load-seed:
	@echo ">> Starting server with load-test rate limits..."
	$(DC) -f docker-compose.yml -f docker-compose.loadtest.yml up -d
	@$(MAKE) --no-print-directory _wait-server
	@bash load_tests/seed.sh

_load-run:
	@echo ">> [1/3] Track vote load test"
	$(K6) run --env BASE_URL=$(API_URL) load_tests/track_vote.js
	@echo ">> [2/3] Delegation load test"
	$(K6) run --env BASE_URL=$(API_URL) load_tests/delegation.js
	@echo ">> [3/3] Playlist editor load test"
	$(K6) run --env BASE_URL=$(API_URL) load_tests/playlist_editor.js

## ---- Internal helpers -----------------------------------------------------

_ensure-device:
	@test -n "$(DEVICE)" || { echo "!! No Android device detected. Connect one (USB debugging on), check 'make devices', or pass DEVICE=<id>."; exit 1; }

# Intentionally advisory (does not fail the build): a device on wifi adb or a
# server reached by IP does not need the reverse, so a failure only warns.
_adb-reverse:
	@$(ADB) -s $(DEVICE) reverse tcp:8081 tcp:8081 >/dev/null 2>&1 && \
	 $(ADB) -s $(DEVICE) reverse tcp:8025 tcp:8025 >/dev/null 2>&1 && \
	 echo ">> adb reverse set: device localhost:8081 -> server, localhost:8025 -> Mailpit" || \
	 echo "!! adb reverse failed. Is the device connected and the stack up ('make up')?"

_wait-server:
	@printf ">> Waiting for the server on $(HEALTH_URL) "; \
	for i in $$(seq 1 60); do \
	  if curl -fsS $(HEALTH_URL) >/dev/null 2>&1; then echo "ok"; exit 0; fi; \
	  printf "."; sleep 1; \
	done; \
	echo ""; echo "!! Server not healthy after 60s. Check 'make logs'."; exit 1

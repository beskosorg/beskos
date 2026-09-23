# Beskos -- builds the apps (desktop, Android, iOS) via Tauri 2.
# The app is the frontend (webmail): source in `frontend/`, the native project in `frontend/src-tauri`.
# Mobile prerequisites and signing: see frontend/src-tauri/MOBILE.md.

FRONTEND := frontend
TAURI    := ./node_modules/.bin/tauri
# ARGS forwards flags to Tauri, e.g.: make android ARGS="--apk --target aarch64"
ARGS     ?=

# URLs embedded in the app (VITE_*), per mode. Production = the public SaaS; dev = local backend.
# (Embedded in plain text in the package -- public URLs only, never secrets.)
PROD_API := https://app.beskos.com
PROD_ENV := VITE_API_BASE=$(PROD_API) VITE_PUBLIC_BASE=$(PROD_API)
DEV_ENV  := VITE_API_BASE=http://localhost:8000 VITE_PUBLIC_BASE=http://localhost:5173

.DEFAULT_GOAL := help
.PHONY: help deps web web-dev desktop desktop-prod desktop-dev desktop-run android ios android-dev ios-dev icons doctor clean mobile

help: ## Shows the available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
	@echo "\n  E.g.: make desktop-prod   make desktop-dev   make android ARGS=\"--apk\""

deps: ## Installs the frontend dependencies (npm --legacy-peer-deps)
	cd $(FRONTEND) && npm install --legacy-peer-deps

web: ## Builds only the web frontend (dist/) -- no packaging
	cd $(FRONTEND) && npm run build

web-dev: ## Local frontend against the PRODUCTION API (localhost:5173, real data)
	@echo "  frontend at http://localhost:5173 - API: $(PROD_API)"
	@echo "  WARNING: REAL data. Deleting, removing, or sending from here actually happens."
	@echo
	cd $(FRONTEND) && env -u VITE_API_BASE API_URL=$(PROD_API) npm run dev

## --- Desktop (macOS/Windows/Linux, matching the machine that runs it) ---
desktop: desktop-prod ## Shortcut for desktop-prod (production build)

desktop-prod: ## PRODUCTION desktop build -- points at app.beskos.com (.dmg/.msi/.deb...)
	cd $(FRONTEND) && $(PROD_ENV) $(TAURI) build $(ARGS)

desktop-dev: ## DEVELOPMENT desktop build -- points at the local backend (localhost)
	cd $(FRONTEND) && $(DEV_ENV) $(TAURI) build $(ARGS)

desktop-run: ## Runs the desktop app in dev mode (hot-reload, local backend)
	cd $(FRONTEND) && $(TAURI) dev $(ARGS)

## --- Android (needs ANDROID_HOME + NDK_HOME -- see MOBILE.md) ---
android: ## Builds the Android APK/AAB (production)
	cd $(FRONTEND) && $(PROD_ENV) $(TAURI) android build $(ARGS)

android-dev: ## Runs on the Android emulator/device (hot-reload)
	cd $(FRONTEND) && $(TAURI) android dev $(ARGS)

## --- iOS (macOS only; signing needs an Apple Developer account) ---
ios: ## Builds the iOS IPA (production)
	cd $(FRONTEND) && $(PROD_ENV) $(TAURI) ios build $(ARGS)

ios-dev: ## Runs on the iOS simulator/device (hot-reload)
	cd $(FRONTEND) && $(TAURI) ios dev $(ARGS)

icons: ## Regenerates the icons from src-tauri/icons/icon.png
	cd $(FRONTEND) && $(TAURI) icon src-tauri/icons/icon.png

mobile: android ios ## Builds Android and iOS (iOS skipped outside macOS)

doctor: ## Checks prerequisites (tauri info)
	cd $(FRONTEND) && $(TAURI) info

clean: ## Cleans build artifacts (dist + the Rust target dir)
	rm -rf $(FRONTEND)/dist
	cd $(FRONTEND)/src-tauri && cargo clean

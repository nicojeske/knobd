.PHONY: build release check-static test vet lint fmt fmt-check schema run monitor monitor-audio calibrate-leds install-user uninstall-user clean ui-install ui-dev ui-icons ui-codegen ui-test ui-lint ui-build

DAEMON_DIR := daemon
BIN := $(DAEMON_DIR)/knobd

# Where install-user puts things. The systemd unit's ExecStart is
# rewritten to $(PREFIX)/bin/knobd at install time (see install-user
# below); packaging/arch's PKGBUILD installs to /usr/bin instead and
# ships the unit unmodified.
PREFIX  ?= $(HOME)/.local
UNITDIR ?= $(HOME)/.config/systemd/user

build: ## Build the daemon binary
	cd $(DAEMON_DIR) && go build -o knobd ./cmd/knobd

release: ## Build a static, stripped release binary (ADR 0001: no CGo)
	cd $(DAEMON_DIR) && CGO_ENABLED=0 go build -trimpath -mod=readonly -ldflags "-s -w" -o knobd ./cmd/knobd

check-static: release ## Fail unless the release binary is statically linked
	@if ldd $(BIN) >/dev/null 2>&1; then \
		echo "$(BIN) is dynamically linked:"; \
		ldd $(BIN); \
		exit 1; \
	else \
		echo "$(BIN): not a dynamic executable (static, per ADR 0001)"; \
	fi
# ldd exits nonzero and prints "not a dynamic executable" for a static
# binary; it exits 0 and lists shared objects for a dynamic one -- the
# condition above relies on that exit code, not the printed text.

test: ## Run daemon unit tests
	cd $(DAEMON_DIR) && go test ./...

vet: ## go vet the daemon
	cd $(DAEMON_DIR) && go vet ./...

fmt: ## gofmt-fix the daemon
	cd $(DAEMON_DIR) && gofmt -w -l .

fmt-check: ## Fail if gofmt would change anything
	@cd $(DAEMON_DIR) && test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)

lint: fmt-check vet ## fmt-check + vet, plus golangci-lint if installed
	@if command -v golangci-lint >/dev/null 2>&1; then \
		cd $(DAEMON_DIR) && golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed; ran gofmt+vet only"; \
	fi

schema: ## Regenerate docs/{config.schema,openapi,device-layout}.json (see daemon/internal/schema)
	cd $(DAEMON_DIR) && go run ./cmd/schemagen -kind config -o ../docs/config.schema.json
	cd $(DAEMON_DIR) && go run ./cmd/schemagen -kind openapi -o ../docs/openapi.json
	cd $(DAEMON_DIR) && go run ./cmd/schemagen -kind device-layout -o ../docs/device-layout.json

run: build ## Run the daemon in the foreground with debug logging
	./$(BIN) --log-level debug

monitor: build ## Print decoded MIDI events without running the full daemon
	./$(BIN) monitor

monitor-audio: build ## Print live PipeWire sinks/sources/streams and change events
	./$(BIN) monitor-audio

monitor-focus: build ## Print focus changes as KWin reports them
	./$(BIN) monitor-focus

calibrate-leds: build ## Build knobd for LED calibration (run e.g. ./daemon/knobd calibrate-leds -cc 48 -value 0)
	@echo "built $(BIN); run e.g.:"
	@echo "    ./$(BIN) calibrate-leds -cc 48 -value 0     # encoder 1's ring"
	@echo "    ./$(BIN) calibrate-leds -note 89 -velocity 1  # button 1's LED"

install-user: build ## Install knobd + the systemd user unit into ~/.local (dev path; packaged installs use packaging/arch)
	install -Dm755 $(BIN) $(PREFIX)/bin/knobd
	install -dm755 $(UNITDIR)
	sed 's|^ExecStart=.*|ExecStart=$(PREFIX)/bin/knobd|' packaging/systemd/knobd.service > $(UNITDIR)/knobd.service
	chmod 644 $(UNITDIR)/knobd.service
	systemctl --user daemon-reload
	@echo "installed $(PREFIX)/bin/knobd and $(UNITDIR)/knobd.service"
	@echo "enable and start it with:"
	@echo "    systemctl --user enable --now knobd.service"

uninstall-user: ## Remove what install-user installed
	-systemctl --user disable --now knobd.service
	rm -f $(UNITDIR)/knobd.service $(PREFIX)/bin/knobd
	systemctl --user daemon-reload

ui-install: ## Install UI dependencies
	cd ui && npm install

ui-dev: ## Run the UI in dev mode (requires ui-install and a Rust toolchain)
	cd ui && npm run tauri dev

ui-icons: ## Regenerate ui/src-tauri/icons/ from icons/source/*.svg
	cd ui && npx tauri icon src-tauri/icons/source/knobd.svg
	rm -rf ui/src-tauri/icons/android ui/src-tauri/icons/ios ui/src-tauri/icons/Square*.png ui/src-tauri/icons/StoreLogo.png ui/src-tauri/icons/64x64.png
	rsvg-convert -w 32 -h 32 ui/src-tauri/icons/source/tray.svg -o ui/src-tauri/icons/tray.png

ui-codegen: ## Regenerate ui/src/types/{config,api}.ts
	cd ui && npm run codegen

ui-test: ## Run the UI's vitest suite plus the Rust bridge's cargo test
	cd ui && npm test
	cd ui/src-tauri && cargo test

ui-lint: ## eslint + prettier --check (TS) and fmt/clippy (Rust)
	cd ui && npm run lint
	cd ui && npm run format:check
	cd ui/src-tauri && cargo fmt --check
	cd ui/src-tauri && cargo clippy --all-targets -- -D warnings

ui-build: ## Production Tauri build (produces a .deb; packaging/arch builds --no-bundle instead)
	cd ui && npm run tauri build

clean:
	rm -f $(BIN)
	rm -rf ui/dist ui/src-tauri/target

.PHONY: build test vet lint fmt fmt-check schema run monitor monitor-audio clean ui-install ui-dev

DAEMON_DIR := daemon
BIN := $(DAEMON_DIR)/knobd

build: ## Build the daemon binary
	cd $(DAEMON_DIR) && go build -o knobd ./cmd/knobd

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

schema: ## Regenerate docs/config.schema.json (see daemon/internal/schema)
	cd $(DAEMON_DIR) && go run ./cmd/schemagen -o ../docs/config.schema.json

run: build ## Run the daemon in the foreground with debug logging
	./$(BIN) --log-level debug

monitor: build ## Print decoded MIDI events without running the full daemon
	./$(BIN) monitor

monitor-audio: build ## Print live PipeWire sinks/sources/streams and change events
	./$(BIN) monitor-audio

ui-install: ## Install UI dependencies
	cd ui && npm install

ui-dev: ## Run the UI in dev mode (requires ui-install and a Rust toolchain)
	cd ui && npm run tauri dev

clean:
	rm -f $(BIN)
	rm -rf ui/dist ui/src-tauri/target

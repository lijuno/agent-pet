.PHONY: help deps dev build build-tray petctl pets test vet fmt clean run-headless demo

BIN := bin
VERSION ?= 0.1.0
LDFLAGS := -X main.version=$(VERSION)

help:
	@echo "make deps           resolve Go modules (run this first)"
	@echo "make dev            run the pet with hot reload"
	@echo "make build          build the .app bundle"
	@echo "make build-tray     build with the optional status-bar icon"
	@echo "make petctl         build the CLI into $(BIN)/"
	@echo "make test           run the test suite"
	@echo "make pets           regenerate the built-in sprite art"
	@echo "make demo           drive the pet through a realistic session"

deps:
	go mod tidy

dev:
	wails dev -ldflags "$(LDFLAGS)"

build:
	wails build -clean -ldflags "$(LDFLAGS)"

# See ADR 0005. Opt-in because it shares the Cocoa run loop with Wails.
build-tray:
	wails build -clean -tags tray -ldflags "$(LDFLAGS)"

petctl:
	@mkdir -p $(BIN)
	go build -ldflags "$(LDFLAGS)" -o $(BIN)/petctl ./cmd/petctl

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

pets:
	python3 tools/genpets/genpets.py

clean:
	rm -rf $(BIN) build/bin

demo: petctl
	@./scripts/demo.sh

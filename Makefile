.PHONY: help deps dev build petctl pets test test-ui vet fmt clean run-headless demo
BIN := bin
VERSION ?= 0.1.0
LDFLAGS := -X main.version=$(VERSION)
help:
	@echo "make deps           resolve Go modules (run this first)"
	@echo "make dev            run the pet with hot reload"
	@echo "make build          build the .app bundle"
	@echo "make petctl         build the CLI into $(BIN)/"
	@echo "make test           run the Go test suite"
	@echo "make test-ui        open the UI tests in a browser"
	@echo "make pets           regenerate the built-in sprite art"
	@echo "make demo           drive the pet through a realistic session"
deps:
	go mod tidy
dev:
	wails dev -ldflags "$(LDFLAGS)"
build:
	wails build -clean -ldflags "$(LDFLAGS)"
# BROKEN: fyne.io/systray declares an Objective-C class named AppDelegate and
# so does Wails' own desktop frontend, so a production build fails to link with
# a duplicate symbol. `go build -tags tray` links only because it leaves the
# desktop frontend out. See ADR 0005.
build-tray:
	wails build -clean -tags tray -ldflags "$(LDFLAGS)"
petctl:
	@mkdir -p $(BIN)
	go build -ldflags "$(LDFLAGS)" -o $(BIN)/petctl ./cmd/petctl
test:
	go test ./...
# The UI tests load ui/dist/index.html into an iframe, so they need a real
# browser and a real origin — file:// blocks same-origin access to the frame.
# Any static server works; this one is already on every Mac.
UI_TEST_PORT ?= 8899
test-ui:
	@python3 -m http.server $(UI_TEST_PORT) --bind 127.0.0.1 & \
	  server=$$!; sleep 1; \
	  open "http://127.0.0.1:$(UI_TEST_PORT)/ui/test/index.html"; \
	  echo "serving on $(UI_TEST_PORT) — results are on the page, ctrl-c to stop"; \
	  trap "kill $$server" INT TERM; wait $$server
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

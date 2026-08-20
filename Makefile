.PHONY: help deps dev build test-desktop petctl pets test test-ui vet fmt clean run-headless demo embed-petctl version-sync
BIN := bin
APP := build/bin/digital-pet.app

# The tag is the single source of truth for the version. wails.json carries a
# copy because Info.plist is templated from it; `make version-sync` rewrites it
# from here, and the release workflow runs that before building so a release
# cannot ship claiming a version nobody tagged.
GIT_VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
VERSION ?= $(if $(GIT_VERSION),$(GIT_VERSION),0.1.0)
LDFLAGS := -X main.version=$(VERSION)
help:
	@echo "make deps           resolve Go modules (run this first)"
	@echo "make dev            run the pet with hot reload"
	@echo "make build          build the .app bundle"
	@echo "make petctl         build the CLI into $(BIN)/"
	@echo "make test           run the Go test suite"
	@echo "make test-ui        open the UI tests in a browser"
	@echo "make test-desktop   check the menu bar and window placement (app must be running)"
	@echo "make pets           regenerate the built-in sprite art"
	@echo "make demo           drive the pet through a realistic session"
	@echo "make version-sync    write $(VERSION) into wails.json before a release"
deps:
	go mod tidy
dev:
	wails dev -ldflags "$(LDFLAGS)"
# darwin/universal because half the audience is on Intel and a release that
# silently excludes them looks identical to one that does not.
build:
	wails build -clean -platform darwin/universal -ldflags "$(LDFLAGS)"
	@$(MAKE) --no-print-directory embed-petctl
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
# The menu bar and where a menu lands in a corner cannot be unit-tested and
# cannot be seen from a test. This drives a running app and checks its answers.
test-desktop:
	@./scripts/desktop-test.sh

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

# petctl ships inside the bundle so the plugin's bin/petctl shim has something
# to resolve, and so the CLI and the petd it talks to can never be different
# builds. Cross-compiled and lipo'd rather than taken from `make petctl`, which
# builds only for this machine.
embed-petctl:
	@test -d "$(APP)" || { echo "no bundle at $(APP) — wails build failed, and a failed build leaves the previous one in place"; exit 1; }
	@tmp=$$(mktemp -d); \
	  CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $$tmp/amd64 ./cmd/petctl && \
	  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $$tmp/arm64 ./cmd/petctl && \
	  lipo -create -output "$(APP)/Contents/MacOS/petctl" $$tmp/amd64 $$tmp/arm64; \
	  rc=$$?; rm -rf $$tmp; test $$rc -eq 0
	@echo "embedded petctl $(VERSION) ($$(lipo -archs '$(APP)/Contents/MacOS/petctl'))"

# Rewrites wails.json's productVersion from VERSION. Info.plist is templated
# from that field, so this is what makes CFBundleShortVersionString match the
# tag. Intended for the release workflow; it edits a tracked file.
version-sync:
	@python3 -c "import json,sys; p='wails.json'; d=json.load(open(p)); d['info']['productVersion']='$(VERSION)'; json.dump(d,open(p,'w'),indent=2); open(p,'a').write('\n'); print('wails.json productVersion ->','$(VERSION)')"

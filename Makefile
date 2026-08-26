.PHONY: help deps dev build require-wails test-desktop petctl pets icons test test-ui vet fmt clean run-headless demo notarize release plugin-hooks plugin-validate embed-petctl version-sync states-gif
BIN := bin

# CHANNEL picks which of the two applications to build. They install side by
# side and each follows its own update channel, so "switching channel" means
# running the other app (ADR 0008). Everything that differs between them lives
# in internal/flavor; nothing here repeats those values, it only passes the
# stamp along and asks the built binary what it became.
CHANNEL ?= release
ifeq ($(CHANNEL),release)
APP_SLUG := agent-pet
FLAVOR_LDFLAGS :=
else ifeq ($(CHANNEL),dev)
APP_SLUG := agent-pet-dev
FLAVOR_LDFLAGS := -X github.com/lijuno/agent-pet/internal/flavor.Name=dev
else
$(error CHANNEL must be release or dev, not "$(CHANNEL)")
endif
APP := build/bin/$(APP_SLUG).app

# The tag is the single source of truth for the version. wails.json carries a
# copy because Info.plist is templated from it; `make version-sync` rewrites it
# from here, and cutting a release means running it before `make build` so a
# release cannot ship claiming a version nobody tagged.
GIT_VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
VERSION ?= $(if $(GIT_VERSION),$(GIT_VERSION),0.1.0)
LDFLAGS := -X main.version=$(VERSION) $(FLAVOR_LDFLAGS)

# `go install` puts wails in $(go env GOPATH)/bin, and nothing puts that
# directory on PATH — on macOS it is not there by default. The README tells you
# to run exactly that install, so the Makefile looks where it landed rather
# than reporting `wails: command not found` for a binary that is sitting on
# disk. PATH still wins, so a wails installed anywhere else is used as-is.
GO_BIN := $(shell go env GOBIN 2>/dev/null)
ifeq ($(strip $(GO_BIN)),)
GOPATH_DIR := $(shell go env GOPATH 2>/dev/null)
# Not just $(GOPATH_DIR)/bin: with go missing that expands to the string "/bin",
# and the error below then tells you to put /bin on your PATH.
ifeq ($(strip $(GOPATH_DIR)),)
GO_BIN := $(HOME)/go/bin
else
GO_BIN := $(GOPATH_DIR)/bin
endif
endif
WAILS := $(shell command -v wails 2>/dev/null || echo $(GO_BIN)/wails)
help:
	@echo "make deps           resolve Go modules (run this first)"
	@echo "make dev            run the pet with hot reload"
	@echo "make build          build the .app bundle (CHANNEL=dev for the dev app)"
	@echo "make petctl         build the CLI into $(BIN)/"
	@echo "make test           run the Go test suite"
	@echo "make test-ui        open the UI tests in a browser"
	@echo "make test-desktop   check the menu bar and window placement (app must be running)"
	@echo "make pets           regenerate the built-in sprite art"
	@echo "make demo           drive the pet through a realistic session"
	@echo "make plugin-hooks   regenerate the plugin hooks from the adapter"
	@echo "make plugin-validate check the plugin and marketplace manifests"
	@echo "make version-sync    write $(VERSION) into wails.json before a release"
	@echo "make states-gif     rebuild the README state figures from the sprites"
	@echo "make notarize       sign, notarize and staple the built bundle"
	@echo "make release        build, notarize, publish, and write the update manifest"
	@echo "make icons          regenerate the dev app's badged icons (needs Pillow)"
deps:
	go mod tidy
dev: require-wails
	$(WAILS) dev -ldflags "$(LDFLAGS)"
# darwin/universal because half the audience is on Intel and a release that
# silently excludes them looks identical to one that does not.
build: require-wails
	$(WAILS) build -clean -platform darwin/universal -ldflags "$(LDFLAGS)"
	@./scripts/brand.sh $(CHANNEL)
	@$(MAKE) --no-print-directory embed-petctl CHANNEL=$(CHANNEL)
# BROKEN: fyne.io/systray declares an Objective-C class named AppDelegate and
# so does Wails' own desktop frontend, so a production build fails to link with
# a duplicate symbol. `go build -tags tray` links only because it leaves the
# desktop frontend out. See ADR 0005.
build-tray: require-wails
	$(WAILS) build -clean -tags tray -ldflags "$(LDFLAGS)"
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

# The dev app's menu-bar and Finder icons, derived from the release ones. Both
# are committed, so building the dev app needs no Pillow — only regenerating
# them does. Two apps in the menu bar with the same icon would be two apps you
# cannot tell apart.
icons:
	@python3 scripts/gen-trayicon-dev.py
	@python3 scripts/gen-appicon-dev.py
clean:
	rm -rf $(BIN) build/bin
demo: petctl
	@./scripts/demo.sh

# plugin/hooks/hooks.json has to agree with claude.Hooks exactly. Generating it
# is the only way they stay in step; adapters/claude/plugin_hooks_test.go
# fails if they drift.
plugin-hooks:
	@python3 scripts/gen-plugin-hooks.py

# What the community-marketplace review pipeline runs, so a rejection is found
# here rather than on submission.
plugin-validate:
	@claude plugin validate ./plugin
	@claude plugin validate .

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
# tag. Run it before `make build` when cutting a release; it edits a tracked
# file.
version-sync:
	@python3 -c "import json,sys; p='wails.json'; d=json.load(open(p)); d['info']['productVersion']='$(VERSION)'; json.dump(d,open(p,'w'),indent=2); open(p,'a').write('\n'); print('wails.json productVersion ->','$(VERSION)')"

# The README's state figure. Generated from the shipped sprites and manifest so
# it cannot drift into advertising an animation the pet does not have.
# One figure per shipped character. All of them, because a README that shows
# one of two characters is a README that will show one of three.
states-gif:
	@python3 scripts/make-states-gif.py --pet sanmao
	@python3 scripts/make-states-gif.py --pet peach
	@python3 scripts/make-states-gif.py --pet juanmao
	@python3 scripts/make-states-gif.py --pet maomao
	@python3 scripts/make-states-gif.py --pet damao

# Fails with the command that fixes it, rather than "command not found" for a
# binary that may well be installed.
require-wails:
	@test -x "$(WAILS)" || { \
	  echo "wails not found (looked on PATH and in $(GO_BIN))"; \
	  echo; \
	  echo "  go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2"; \
	  echo; \
	  echo "If that already ran, $(GO_BIN) is not on your PATH:"; \
	  echo "  export PATH=\"$$PATH:$(GO_BIN)\""; \
	  exit 1; }

# Signs, notarizes and staples whatever `make build` produced. Needs a
# Developer ID Application certificate and a notarytool keychain profile; the
# script says how to get both if either is missing. The profile name comes from
# .notary-profile, which is machine-local and git-ignored; NOTARY_PROFILE
# overrides it for one run. Releases are cut by hand on the machine holding the
# certificate — there is no signing job in CI, deliberately, because there
# would have to be a copy of the private key in the repository secrets.
notarize:
	@./scripts/notarize.sh $(if $(NOTARY_PROFILE),--profile $(NOTARY_PROFILE),)

# Builds, notarizes, uploads the asset, and writes updates/$(CHANNEL).json —
# without committing it. Publishing an asset and offering it over the air are
# two separate acts: the second is the commit, which you make by hand after
# reading the diff. CHANNEL=dev publishes prereleases. See ADR 0008.
release:
	@./scripts/release.sh $(if $(CHANNEL),--channel $(CHANNEL),)

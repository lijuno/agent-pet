#!/bin/bash
# Prepares a Claude Code on the web container for this repository.
#
# The container is cloned fresh and thrown away, so everything the three test
# suites need has to be here before the first tool call. What that comes to is
# the Go module cache, a warm build cache, and Pillow — which nothing but the
# sprite generator needs, and which `make pets` needs before it can draw a
# single pixel.
#
# What it deliberately does not install is the Wails CLI. `make build` is
# darwin/universal and the menu-bar item is Objective-C through cgo; a Linux
# container cannot produce the app whatever is installed, so fetching the CLI
# would only buy a slower start and a more convincing-looking failure.
set -euo pipefail

# Local sessions have a machine somebody set up already; this is about the
# throwaway one.
if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
	exit 0
fi

cd "${CLAUDE_PROJECT_DIR:-$(dirname "$0")/../..}"

echo "agent-pet: preparing the container"

# download rather than tidy: tidy rewrites go.mod and go.sum, and a session that
# begins by modifying tracked files starts with a diff nobody asked for.
go mod download

# The first `go test ./...` otherwise compiles Wails from cold, which is most of
# a minute of a tool call that looks like it has hung. The container is cached
# after this hook, so it is paid once.
go build ./... >/dev/null

# The toolchain go.mod claims, which is not the one this container ships. CI
# pins it, and a stdlib call newer than it compiles here without a word and
# fails there — `make vet-min` is the check, and this is what makes it cheap.
go_min=$(sed -n 's/^go //p' go.mod)
if [ -n "$go_min" ]; then
	GOTOOLCHAIN="go$go_min" go version >/dev/null 2>&1 &&
		echo "agent-pet: go$go_min available for 'make vet-min'" ||
		echo "agent-pet: could not fetch go$go_min — 'make vet-min' will try again"
fi

# Only `make pets` and `make states-gif` need this, and only when the art
# changes — so it is worth having, and not worth failing the session over.
if python3 -c "import PIL" 2>/dev/null; then
	echo "agent-pet: Pillow already present"
elif python3 -m pip install --quiet --disable-pip-version-check Pillow 2>/dev/null; then
	echo "agent-pet: installed Pillow (make pets)"
else
	echo "agent-pet: no Pillow — 'make pets' will not run, everything else will"
fi

# Say what this box can and cannot do, once, where the session will read it.
cat <<'NOTE'
agent-pet: ready.
  runs here:  go test ./...   go vet ./...   gofmt -l .
              make vet-min    make pets       make test-ui-headless
  before you push Go: make vet-min. This box has a newer Go than go.mod
              claims, and only the claimed one fails the way CI fails.
  needs a Mac: make build (wails, darwin/universal, cgo Objective-C)
               make test-desktop (drives the running app's menu bar)
  See the "Working in the cloud" section of CLAUDE.md.
NOTE

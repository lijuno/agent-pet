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
              make pets       make test-ui-headless
  needs a Mac: make build (wails, darwin/universal, cgo Objective-C)
               make test-desktop (drives the running app's menu bar)
  See the "Working in the cloud" section of CLAUDE.md.
NOTE

BIN     := bin/dpad
PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X dpad/cmd.version=$(VERSION)

# Prefer the compose plugin, fall back to standalone docker-compose.
COMPOSE ?= $(shell docker compose version >/dev/null 2>&1 && echo "docker compose" || echo docker-compose)
DEMO    := $(COMPOSE) -f testenv/compose.yaml

GOLANGCI_LINT ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
GOVULNCHECK   ?= golang.org/x/vuln/cmd/govulncheck@v1.9.0

.PHONY: build install uninstall run test vet lint vuln clean demo-up demo-extras demo-down demo-status screenshots website

# Static, reproducible build: no cgo, no local paths in the binary.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) .

# Installs dpad plus a short `dp` symlink into $(PREFIX)/bin.
install: build
	install -d $(PREFIX)/bin
	install -m 0755 $(BIN) $(PREFIX)/bin/dpad
	ln -sf dpad $(PREFIX)/bin/dp

uninstall:
	rm -f $(PREFIX)/bin/dpad $(PREFIX)/bin/dp

# Run from source without installing: make run ARGS="stats --sort mem"
run:
	go run . $(ARGS)

test:
	go test ./...

vet:
	go vet ./...

lint:
	go run $(GOLANGCI_LINT) run ./...

vuln:
	go run $(GOVULNCHECK) ./...

clean:
	rm -rf bin

# ── Demo environment (testenv/) ──────────────────────────────────────────────

demo-up:
	$(DEMO) up -d
	@$(MAKE) --no-print-directory demo-extras
	@echo
	@echo "Demo running. Try: dp, dp ps -a, dp stats, dp logs -f dp-demo-logger-text-1 dp-demo-logger-json-1"

# States compose can't express: paused, created (never started), dangling image.
# The dangling image is made by force-untagging an image a container still
# uses; simply rebuilding a tag doesn't leave one behind with the containerd store.
demo-extras:
	-$(DEMO) pause paused
	-docker rm -f dp-demo-created >/dev/null 2>&1
	printf 'FROM alpine:3.20\nLABEL dp-demo=true\n' | docker build -q -t dp-demo/dangling:latest - >/dev/null
	docker create --name dp-demo-created --label dp-demo=true dp-demo/dangling:latest true >/dev/null
	docker rmi -f dp-demo/dangling:latest >/dev/null

demo-down:
	-$(DEMO) unpause paused
	$(DEMO) down -v --remove-orphans
	-docker rm -f dp-demo-created
	-docker image prune -f --filter label=dp-demo=true

demo-status:
	$(DEMO) ps -a

# Regenerates docs/images from the running demo; see scripts/screenshots/README.md.
screenshots:
	scripts/screenshots/capture.sh

# Copies docs/images into the dpad.sh website and makes the light-mode copies;
# see website/README.md. Run it after `make screenshots`.
website:
	go run ./website/sync

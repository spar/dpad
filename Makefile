BIN     := bin/dockernext
PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X dockernext/cmd.version=$(VERSION)

# Prefer the compose plugin, fall back to standalone docker-compose.
COMPOSE ?= $(shell docker compose version >/dev/null 2>&1 && echo "docker compose" || echo docker-compose)
DEMO    := $(COMPOSE) -f testenv/compose.yaml

GOLANGCI_LINT ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
GOVULNCHECK   ?= golang.org/x/vuln/cmd/govulncheck@v1.9.0

.PHONY: build install uninstall run test vet lint vuln clean demo-up demo-extras demo-down demo-status screenshots

# Static, reproducible build: no cgo, no local paths in the binary.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) .

# Installs dockernext plus a short `dn` symlink into $(PREFIX)/bin.
install: build
	install -d $(PREFIX)/bin
	install -m 0755 $(BIN) $(PREFIX)/bin/dockernext
	ln -sf dockernext $(PREFIX)/bin/dn

uninstall:
	rm -f $(PREFIX)/bin/dockernext $(PREFIX)/bin/dn

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
	@echo "Demo running. Try: dn, dn ps -a, dn stats, dn logs -f dn-demo-logger-text-1 dn-demo-logger-json-1"

# States compose can't express: paused, created (never started), dangling image.
# The dangling image is made by force-untagging an image a container still
# uses; simply rebuilding a tag doesn't leave one behind with the containerd store.
demo-extras:
	-$(DEMO) pause paused
	-docker rm -f dn-demo-created >/dev/null 2>&1
	printf 'FROM alpine:3.20\nLABEL dn-demo=true\n' | docker build -q -t dn-demo/dangling:latest - >/dev/null
	docker create --name dn-demo-created --label dn-demo=true dn-demo/dangling:latest true >/dev/null
	docker rmi -f dn-demo/dangling:latest >/dev/null

demo-down:
	-$(DEMO) unpause paused
	$(DEMO) down -v --remove-orphans
	-docker rm -f dn-demo-created
	-docker image prune -f --filter label=dn-demo=true

demo-status:
	$(DEMO) ps -a

# Regenerates docs/images from the running demo; see scripts/screenshots/README.md.
screenshots:
	scripts/screenshots/capture.sh

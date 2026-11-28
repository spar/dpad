BIN     := bin/dpad
PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/spar/dpad/cmd.version=$(VERSION)

# Prefer the compose plugin, fall back to standalone docker-compose.
COMPOSE ?= $(shell docker compose version >/dev/null 2>&1 && echo "docker compose" || echo docker-compose)
# One Compose project per folder under testenv/.
DEMO_PROJECTS := bookshop monitoring homelab
demo = $(COMPOSE) -f testenv/$(1)/compose.yaml

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

# go run of a pinned tool ignores go.mod's toolchain; build the tools with it
# so they can read code written for it.
TOOLCHAIN = GOTOOLCHAIN=$$(go env GOVERSION)

lint:
	$(TOOLCHAIN) go run $(GOLANGCI_LINT) run ./...

vuln:
	$(TOOLCHAIN) go run $(GOVULNCHECK) ./...

clean:
	rm -rf bin

# ── Demo environment (testenv/) ──────────────────────────────────────────────

demo-up:
	$(foreach p,$(DEMO_PROJECTS),$(call demo,$(p)) up -d &&) true
	@$(MAKE) --no-print-directory demo-extras
	@echo
	@echo "Demo running. Try: dp, dp ps -a, dp stats, dp logs -f bookshop-api-1 bookshop-billing-1"

# States compose can't express: paused, created (never started), dangling image.
# The dangling image is made by force-untagging an image a container still
# uses; simply rebuilding a tag doesn't leave one behind with the containerd store.
demo-extras:
	-$(call demo,homelab) pause uptime-kuma
	-docker rm -f api-canary >/dev/null 2>&1
	printf 'FROM alpine:3.20\nLABEL dpad.demo=true\n' | docker build -q -t bookshop/api:canary - >/dev/null
	docker create --name api-canary --label dpad.demo=true bookshop/api:canary true >/dev/null
	docker rmi -f bookshop/api:canary >/dev/null

demo-down:
	-$(call demo,homelab) unpause uptime-kuma
	$(foreach p,$(DEMO_PROJECTS),$(call demo,$(p)) down -v --remove-orphans;)
	-docker rm -f api-canary
	-docker image prune -f --filter label=dpad.demo=true

demo-status:
	$(foreach p,$(DEMO_PROJECTS),$(call demo,$(p)) ps -a;)

# Regenerates docs/images from the running demo; see scripts/screenshots/README.md.
screenshots:
	scripts/screenshots/capture.sh

# Builds the dpad.sh pages and sitemap from website/pages, copies docs/images
# into the website and makes the light-mode copies; see website/README.md.
# Run it after `make screenshots` or a change to website/pages.
website:
	go run ./website/sync

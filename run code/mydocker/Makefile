BINDIR     := /usr/local/bin
GO         := go
GOFLAGS    := -trimpath

.PHONY: all build install uninstall clean deps setup test

all: build

## Build tất cả binaries
build: deps
	@echo "Building mydocker..."
	$(GO) build $(GOFLAGS) -o mydocker       ./cmd/mydocker
	$(GO) build $(GOFLAGS) -o mydockerd      ./cmd/mydockerd
	$(GO) build $(GOFLAGS) -o mydocker-compose ./cmd/mydocker-compose
	@echo "Build OK: mydocker, mydockerd, mydocker-compose"

## Download dependencies
deps:
	$(GO) mod download

## Install binaries + setup wrapper scripts
install: build
	@echo "Installing to $(BINDIR)..."
	install -m 755 mydocker        $(BINDIR)/mydocker.real
	install -m 755 mydockerd       $(BINDIR)/mydockerd.real
	install -m 755 mydocker-compose $(BINDIR)/mydocker-compose

	@# Wrapper: tự động sudo khi cần
	printf '#!/bin/bash\nif [ "$$EUID" -ne 0 ]; then exec sudo $(BINDIR)/mydocker.real "$$@"; else exec $(BINDIR)/mydocker.real "$$@"; fi\n' \
		| install -m 755 /dev/stdin $(BINDIR)/mydocker

	printf '#!/bin/bash\nexec sudo $(BINDIR)/mydockerd.real "$$@"\n' \
		| install -m 755 /dev/stdin $(BINDIR)/mydockerd

	@echo "Installed. Run: mydocker run alpine:latest"

## Full setup: install + configure system
setup: install
	bash scripts/setup.sh

## Uninstall
uninstall:
	rm -f $(BINDIR)/mydocker $(BINDIR)/mydocker.real
	rm -f $(BINDIR)/mydockerd $(BINDIR)/mydockerd.real
	rm -f $(BINDIR)/mydocker-compose
	@echo "Uninstalled."

## Clean build artifacts
clean:
	rm -f mydocker mydockerd mydocker-compose

## Run E2E tests
test: build
	@echo "Running E2E tests..."
	sudo bash test/integration/e2e.sh

## Start daemon
daemon:
	mydockerd

## Stop daemon
stop-daemon:
	@pkill -f mydockerd.real || echo "Daemon not running"
	@rm -f /var/run/mydocker.sock

PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
BINARY_NAME ?= tk

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.2.0")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

MODULE := task_cli
LDFLAGS := -X '$(MODULE)/internal/version.Version=$(VERSION)' \
           -X '$(MODULE)/internal/version.Commit=$(COMMIT)' \
           -X '$(MODULE)/internal/version.BuildDate=$(BUILD_DATE)' \
           -w -s

.PHONY: all build install uninstall test test-race migrate restart-daemon version clean

all: build

version:
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(COMMIT)"
	@echo "Build date: $(BUILD_DATE)"

build:
	@echo "==> Building $(BINARY_NAME) ($(VERSION))..."
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/tk

test:
	@echo "==> Running tests..."
	go test ./...

test-race:
	@echo "==> Running tests with race detector..."
	go test -race ./...

# Stop active daemon, build binary with version ldflags, install to ~/.local/bin/tk,
# copy to local repository root, verify/migrate databases, and print version info.
install: build
	@echo "==> Stopping running daemon if active..."
	@-if [ -x "$(BINDIR)/$(BINARY_NAME)" ]; then \
		"$(BINDIR)/$(BINARY_NAME)" daemon stop 2>/dev/null || true; \
	elif [ -x "./$(BINARY_NAME)" ]; then \
		"./$(BINARY_NAME)" daemon stop 2>/dev/null || true; \
	fi
	@-pkill -f "$(BINARY_NAME) daemon run" 2>/dev/null || true
	@mkdir -p $(BINDIR)
	@echo "==> Installing $(BINARY_NAME) to $(BINDIR)/$(BINARY_NAME)..."
	install -m 755 $(BINARY_NAME) $(BINDIR)/$(BINARY_NAME)
	@echo "==> Triggering schema verification & migration..."
	@"$(BINDIR)/$(BINARY_NAME)" -g list >/dev/null 2>&1 || true
	@echo "==> Verifying installation:"
	@"$(BINDIR)/$(BINARY_NAME)" version
	@echo "✓ Successfully installed $(BINARY_NAME) to $(BINDIR)/$(BINARY_NAME)"

restart-daemon:
	@echo "==> Restarting daemon..."
	@-if [ -x "$(BINDIR)/$(BINARY_NAME)" ]; then \
		"$(BINDIR)/$(BINARY_NAME)" daemon stop 2>/dev/null || true; \
	fi
	@-pkill -f "$(BINARY_NAME) daemon run" 2>/dev/null || true
	@sleep 0.2
	@"$(BINDIR)/$(BINARY_NAME)" daemon status

uninstall:
	@echo "==> Uninstalling $(BINARY_NAME)..."
	@-if [ -x "$(BINDIR)/$(BINARY_NAME)" ]; then \
		"$(BINDIR)/$(BINARY_NAME)" daemon stop 2>/dev/null || true; \
	fi
	@-pkill -f "$(BINARY_NAME) daemon run" 2>/dev/null || true
	rm -f $(BINDIR)/$(BINARY_NAME)
	@echo "✓ Removed $(BINDIR)/$(BINARY_NAME)"

clean:
	@echo "==> Cleaning..."
	rm -f $(BINARY_NAME)
	rm -f sqlite.test

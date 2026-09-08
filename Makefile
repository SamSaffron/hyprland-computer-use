BIN := build
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
APP_PACKAGE := github.com/sam-saffron-jarvis/hyprland-computer-use/internal/app
LDFLAGS = -s -w -X $(APP_PACKAGE).Version=$(VERSION) -X $(APP_PACKAGE).Commit=$(COMMIT) -X $(APP_PACKAGE).Date=$(BUILD_DATE)
GO_SOURCES := $(shell find . -type d \( -name .git -o -name build -o -name dist \) -prune -o -type f -name '*.go' -print)
.PHONY: all native setup-native fmt fmt-check vet native-test test clean
all: $(BIN)/hyprland-computer-use native
$(BIN):
	mkdir -p $(BIN)
$(BIN)/hyprland-computer-use: $(GO_SOURCES) $(wildcard native/*) $(wildcard quickshell/*.qml) Makefile LICENSE THIRD_PARTY.md go.mod go.sum | $(BIN)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $@ ./cmd/hyprland-computer-use
native: $(BIN)/guard.so
$(BIN)/guard.so: native/guard.cpp native/input_transaction.hpp native/text_transaction.hpp native/text_keymap.hpp native/text_keyboard.hpp | $(BIN)
	$(CXX) -std=c++23 -shared -fPIC -fno-gnu-unique $$(pkg-config --cflags hyprland libeis-1.0) $< -o $@
setup-native: native $(BIN)/header-version $(BIN)/setup-inspector.so
$(BIN)/setup-inspector.so: native/setup_inspector.cpp | $(BIN)
	$(CXX) -std=c++23 -shared -fPIC -fno-gnu-unique $$(pkg-config --cflags hyprland) $< -o $@
$(BIN)/header-version: native/version.cpp | $(BIN)
	$(CXX) $$(pkg-config --cflags hyprland) $< -o $@

fmt:
	go fmt ./...
fmt-check:
	@files="$$(gofmt -l $(GO_SOURCES))" || exit $$?; \
	if [ -n "$$files" ]; then \
		printf 'Go files need formatting (run make fmt):\n%s\n' "$$files"; exit 1; \
	fi
vet:
	go vet ./...
native-test: $(BIN)/input-transaction-test $(BIN)/text-keyboard-test
	$(BIN)/input-transaction-test
	$(BIN)/text-keyboard-test
$(BIN)/text-keyboard-test: native/text_keyboard_test.cpp native/text_keymap.hpp native/text_keyboard.hpp | $(BIN)
	$(CXX) -std=c++23 -Wall -Wextra -Werror $< -o $@
.PHONY: native-text-wire-test
native-text-wire-test: $(BIN)/text-keyboard-wire-test
	$(BIN)/text-keyboard-wire-test
$(BIN)/text-keyboard-wire-test: native/text_keyboard_test.cpp native/text_keymap.hpp native/text_keyboard.hpp | $(BIN)
	$(CXX) -std=c++23 -Wall -Wextra -Werror -DTEXT_TEST_WAYLAND $$(pkg-config --cflags wayland-server) $< -o $@ $$(pkg-config --libs wayland-server)
$(BIN)/input-transaction-test: native/input_transaction_test.cpp native/input_transaction.hpp native/text_transaction.hpp native/text_keymap.hpp native/text_keyboard.hpp | $(BIN)
	$(CXX) -std=c++23 -Wall -Wextra -Werror $< -o $@
test: fmt-check vet native-test
	go test -race ./...
clean:
	rm -rf build

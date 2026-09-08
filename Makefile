BIN := build
.PHONY: all native setup-native fmt fmt-check vet native-test test clean
all: $(BIN)/hyprland-computer-use native
$(BIN):
	mkdir -p $(BIN)
$(BIN)/hyprland-computer-use: $(wildcard *.go) $(wildcard native/*) $(wildcard quickshell/*.qml) Makefile LICENSE THIRD_PARTY.md go.mod go.sum | $(BIN)
	CGO_ENABLED=0 go build -trimpath -o $@ .
native: $(BIN)/guard.so
$(BIN)/guard.so: native/guard.cpp native/input_transaction.hpp | $(BIN)
	$(CXX) -std=c++23 -shared -fPIC -fno-gnu-unique $$(pkg-config --cflags hyprland libeis-1.0) $< -o $@
setup-native: native $(BIN)/header-version $(BIN)/setup-inspector.so
$(BIN)/setup-inspector.so: native/setup_inspector.cpp | $(BIN)
	$(CXX) -std=c++23 -shared -fPIC -fno-gnu-unique $$(pkg-config --cflags hyprland) $< -o $@
$(BIN)/header-version: native/version.cpp | $(BIN)
	$(CXX) $$(pkg-config --cflags hyprland) $< -o $@

fmt:
	go fmt ./...
fmt-check:
	@files="$$(gofmt -l $(wildcard *.go))" || exit $$?; \
	if [ -n "$$files" ]; then \
		printf 'Go files need formatting (run make fmt):\n%s\n' "$$files"; exit 1; \
	fi
vet:
	go vet ./...
native-test: $(BIN)/input-transaction-test
	$(BIN)/input-transaction-test
$(BIN)/input-transaction-test: native/input_transaction_test.cpp native/input_transaction.hpp | $(BIN)
	$(CXX) -std=c++23 -Wall -Wextra -Werror $< -o $@
test: fmt-check vet native-test
	go test -race ./...
clean:
	rm -rf build

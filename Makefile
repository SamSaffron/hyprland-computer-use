BIN := build
.PHONY: all native test clean
all: $(BIN)/computer-use native
$(BIN):
	mkdir -p $(BIN)
$(BIN)/computer-use: $(wildcard *.go) go.mod go.sum | $(BIN)
	CGO_ENABLED=0 go build -trimpath -o $@ .
native: $(BIN)/guard.so $(BIN)/computer-use-keyboard
$(BIN)/guard.so: native/guard.cpp | $(BIN)
	$(CXX) -std=c++23 -shared -fPIC -fno-gnu-unique $$(pkg-config --cflags hyprland) $< -o $@
$(BIN)/virtual-keyboard-client.h: native/virtual-keyboard-unstable-v1.xml | $(BIN)
	wayland-scanner client-header $< $@
$(BIN)/virtual-keyboard-protocol.c: native/virtual-keyboard-unstable-v1.xml | $(BIN)
	wayland-scanner private-code $< $@
$(BIN)/virtual-pointer-client.h: native/wlr-virtual-pointer-unstable-v1.xml | $(BIN)
	wayland-scanner client-header $< $@
$(BIN)/virtual-pointer-protocol.c: native/wlr-virtual-pointer-unstable-v1.xml | $(BIN)
	wayland-scanner private-code $< $@
$(BIN)/computer-use-keyboard: native/keyboard.c $(BIN)/virtual-pointer-client.h $(BIN)/virtual-pointer-protocol.c $(BIN)/virtual-keyboard-client.h $(BIN)/virtual-keyboard-protocol.c
	$(CC) -Wall -Wextra -Wno-unused-parameter -I$(BIN) native/keyboard.c $(BIN)/virtual-keyboard-protocol.c $(BIN)/virtual-pointer-protocol.c $$(pkg-config --cflags --libs wayland-client xkbcommon) -o $@
test:
	go test -race ./...
	go vet ./...
clean:
	rm -rf build

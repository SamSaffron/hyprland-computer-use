// Hyprland 0.56.2 popup-grab entry point. Resolve the live process image,
// not a filesystem copy: /proc/self/exe may name an unlinked executable after
// a package upgrade. The caller must still enforce the exact header hash.
#pragma once
#include <dlfcn.h>

static void *independentPopupGrabAddress() {
  return dlsym(RTLD_DEFAULT,
               "_ZN17CXDGShellProtocol14addOrStartGrabEN9Hyprutils6Memory"
               "14CSharedPointerI17CXDGPopupResourceEE");
}

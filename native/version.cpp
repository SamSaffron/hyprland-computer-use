// Reports the exact header revision used by the bundled native build.
#include <cstdio>
#include <hyprland/src/version.h>
int main() { std::puts(GIT_COMMIT_HASH); }

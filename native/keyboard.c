#define _GNU_SOURCE
#include "virtual-keyboard-client.h"
#include "virtual-pointer-client.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <unistd.h>
#include <wayland-client.h>
#include <xkbcommon/xkbcommon.h>
static struct zwp_virtual_keyboard_manager_v1 *manager;
static struct zwlr_virtual_pointer_manager_v1 *pointer_manager;
static struct wl_seat *seat;
static void global(void *d, struct wl_registry *r, uint32_t n, const char *i,
                   uint32_t v) {
  if (!strcmp(i, "zwp_virtual_keyboard_manager_v1"))
    manager =
        wl_registry_bind(r, n, &zwp_virtual_keyboard_manager_v1_interface, 1);
  if (!strcmp(i, "zwlr_virtual_pointer_manager_v1"))
    pointer_manager =
        wl_registry_bind(r, n, &zwlr_virtual_pointer_manager_v1_interface, 1);
  if (!strcmp(i, "wl_seat") && !seat)
    seat = wl_registry_bind(r, n, &wl_seat_interface, 1);
}
static void removed(void *d, struct wl_registry *r, uint32_t n) {}
int main() {
  struct wl_display *d = wl_display_connect(NULL);
  if (!d)
    return 1;
  struct wl_registry *r = wl_display_get_registry(d);
  struct wl_registry_listener l = {global, removed};
  wl_registry_add_listener(r, &l, NULL);
  wl_display_roundtrip(d);
  if (!manager || !pointer_manager || !seat)
    return 2;
  // Keep seat capabilities alive; input is delivered only by the compositor
  // guard.
  zwlr_virtual_pointer_manager_v1_create_virtual_pointer(pointer_manager, seat);
  struct xkb_context *c = xkb_context_new(0);
  struct xkb_rule_names rules = {.layout = "us"};
  struct xkb_keymap *k = xkb_keymap_new_from_names(c, &rules, 0);
  char *s = xkb_keymap_get_as_string(k, XKB_KEYMAP_FORMAT_TEXT_V1);
  size_t len = strlen(s) + 1;
  int fd = memfd_create("computer-use-keymap", MFD_CLOEXEC);
  if (fd < 0 || ftruncate(fd, len))
    return 3;
  if (write(fd, s, len) != (ssize_t)len)
    return 4;
  struct zwp_virtual_keyboard_v1 *v =
      zwp_virtual_keyboard_manager_v1_create_virtual_keyboard(manager, seat);
  zwp_virtual_keyboard_v1_keymap(v, WL_KEYBOARD_KEYMAP_FORMAT_XKB_V1, fd, len);
  wl_display_roundtrip(d);
  close(fd);
  free(s);
  xkb_keymap_unref(k);
  xkb_context_unref(c);
  puts("ready");
  fflush(stdout);
  while (wl_display_dispatch(d) != -1) {
  }
  wl_display_disconnect(d);
  return 0;
}

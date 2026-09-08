// Exercise the real text map carrier's Linux fd path without a compositor.
// Only IKeyboard and the owning fd wrapper are stubbed. Optional wire mode
// uses the installed libwayland-server with a private socketpair, no desktop.
#include <cassert>
#include <cerrno>
#include <fcntl.h>
#include <iostream>
#include <memory>
#include <string>
#include <unistd.h>
#include <utility>
#include <vector>
#ifdef TEXT_TEST_WAYLAND
#include <sys/socket.h>
#include <wayland-server-core.h>
#include <wayland-server-protocol.h>
#endif

template <class T> using SP = std::shared_ptr<T>;
namespace Aquamarine {
class IKeyboard;
}
namespace Hyprutils::OS {
class CFileDescriptor {
  int fd = -1;

public:
  CFileDescriptor() = default;
  explicit CFileDescriptor(int value) : fd(value) {}
  CFileDescriptor(const CFileDescriptor &) = delete;
  CFileDescriptor &operator=(const CFileDescriptor &) = delete;
  CFileDescriptor &operator=(CFileDescriptor &&other) {
    if (fd >= 0)
      close(fd);
    fd = std::exchange(other.fd, -1);
    return *this;
  }
  ~CFileDescriptor() {
    if (fd >= 0)
      close(fd);
  }
  bool isValid() const { return fd >= 0; }
  int get() const { return fd; }
};
} // namespace Hyprutils::OS
struct IKeyboard {
  std::string m_xkbKeymapV1String;
  Hyprutils::OS::CFileDescriptor m_xkbKeymapV1FD;
  virtual ~IKeyboard() = default;
  virtual bool isVirtual() = 0;
  virtual SP<Aquamarine::IKeyboard> aq() = 0;
};
#include "text_keyboard.hpp"
#include "text_keymap.hpp"

static void checkMap(int fd, const std::string &map) {
  std::vector<char> bytes(map.size() + 1);
  assert(pread(fd, bytes.data(), bytes.size(), 0) == ssize_t(bytes.size()));
  assert(std::string(bytes.data(), map.size()) == map && bytes.back() == 0);
  const int seals = F_SEAL_WRITE | F_SEAL_GROW | F_SEAL_SHRINK | F_SEAL_SEAL;
  assert((fcntl(fd, F_GET_SEALS) & seals) == seals);
  assert(pwrite(fd, "x", 1, 0) == -1 && errno == EPERM);
}
int main() {
  const auto map = textKeymap({65, 0x754c, 0x1f680});
  int oldFD;
  {
    TextKeyboard keyboard(map);
    oldFD = keyboard.m_xkbKeymapV1FD.get();
    assert(lseek(oldFD, 0, SEEK_CUR) == 0);
    assert(keyboard.m_xkbKeymapV1String == map);
    checkMap(oldFD, map);
  }
  assert(fcntl(oldFD, F_GETFD) == -1 && errno == EBADF);
#ifdef TEXT_TEST_WAYLAND
  int sockets[2];
  assert(socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, sockets) == 0);
  auto display = wl_display_create();
  assert(display);
  auto client = wl_client_create(display, sockets[0]);
  assert(client);
  auto resource = wl_resource_create(client, &wl_keyboard_interface, 7, 2);
  assert(resource);
  {
    TextKeyboard keyboard(map);
    wl_keyboard_send_keymap(resource, WL_KEYBOARD_KEYMAP_FORMAT_XKB_V1,
                            keyboard.m_xkbKeymapV1FD.get(), map.size() + 1);
  } // Original FD is closed BEFORE flush: marshalling must have dup'd it.
  wl_client_flush(client);
  char data[16];
  alignas(cmsghdr) char control[CMSG_SPACE(sizeof(int))] = {};
  iovec iov{data, sizeof(data)};
  msghdr message{};
  message.msg_iov = &iov;
  message.msg_iovlen = 1;
  message.msg_control = control;
  message.msg_controllen = sizeof(control);
  assert(recvmsg(sockets[1], &message, MSG_DONTWAIT) == 16);
  auto cmsg = CMSG_FIRSTHDR(&message);
  assert(cmsg && cmsg->cmsg_level == SOL_SOCKET &&
         cmsg->cmsg_type == SCM_RIGHTS);
  int fd = *reinterpret_cast<int *>(CMSG_DATA(cmsg));
  checkMap(fd, map);
  close(fd);
  close(sockets[1]);
  wl_display_destroy_clients(display);
  wl_display_destroy(display);
  std::cout << "libwayland queued keymap FD survives carrier destruction\n";
#endif
  std::cout << "sealed text keymap file/lifetime tests passed\n";
}

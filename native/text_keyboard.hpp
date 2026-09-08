// Protocol-only map carrier for CWLKeyboardResource::sendKeymap on Hyprland
// 0.56.2. NEVER register this as a device or pass it to
// SeatManager::setKeyboard: doing so would broadcast text-bearing maps to
// unrelated clients.
#pragma once
#include <cerrno>
#include <fcntl.h>
#include <stdexcept>
#include <string>
#include <sys/mman.h>
#include <unistd.h>

class TextKeyboard final : public IKeyboard {
public:
  explicit TextKeyboard(const std::string &map) {
    m_xkbKeymapV1String = map;
    m_xkbKeymapV1FD = Hyprutils::OS::CFileDescriptor(
        memfd_create("computer-use-text", MFD_CLOEXEC | MFD_ALLOW_SEALING));
    if (!m_xkbKeymapV1FD.isValid())
      throw std::runtime_error("text_keymap_fd_failed");
    size_t written = 0;
    while (written < map.size() + 1) {
      auto n = write(m_xkbKeymapV1FD.get(), map.c_str() + written,
                     map.size() + 1 - written);
      if (n < 0 && errno == EINTR)
        continue;
      if (n <= 0)
        throw std::runtime_error("text_keymap_write_failed");
      written += n;
    }
    if (lseek(m_xkbKeymapV1FD.get(), 0, SEEK_SET) < 0)
      throw std::runtime_error("text_keymap_rewind_failed");
    if (fcntl(m_xkbKeymapV1FD.get(), F_ADD_SEALS,
              F_SEAL_SHRINK | F_SEAL_GROW | F_SEAL_WRITE | F_SEAL_SEAL) < 0)
      throw std::runtime_error("text_keymap_seal_failed");
  }
  bool isVirtual() override { return true; }
  SP<Aquamarine::IKeyboard> aq() override { return nullptr; }
};

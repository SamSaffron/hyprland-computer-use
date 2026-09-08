// Pure, self-contained XKB v1 map generator. No user strings, includes, paths,
// compose sequences or layout dependencies enter the keymap syntax.
#pragma once
#include <array>
#include <cstdint>
#include <locale>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

// Ordinary printable US key positions only: no raw Copy/Paste/Cut, function
// or modifier codes. 48 also bounds non-interruptible work/revocation latency.
static constexpr std::array<uint32_t, 48> TEXT_KEY_CODES = {
    2,  3,  4,  5,  6,  7,  8,  9,  10, 11, 12, 13, 16, 17, 18, 19,
    20, 21, 22, 23, 24, 25, 26, 27, 30, 31, 32, 33, 34, 35, 36, 37,
    38, 39, 40, 41, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 57};
static constexpr size_t TEXT_CHUNK_RUNES = TEXT_KEY_CODES.size();
static uint32_t textKeysym(uint32_t scalar) {
  if (scalar == 10)
    return 0xff0d; // Return
  if (scalar == 9)
    return 0xff09; // Tab
  if (scalar < 32 || (scalar >= 0x7f && scalar <= 0x9f) || scalar > 0x10ffff ||
      (scalar >= 0xd800 && scalar <= 0xdfff))
    throw std::runtime_error("invalid_text_scalar");
  return scalar <= 0xff ? scalar : (0x01000000 | scalar);
}
static std::string textKeymap(const std::vector<uint32_t> &scalars) {
  if (scalars.empty() || scalars.size() > TEXT_CHUNK_RUNES)
    throw std::runtime_error("invalid_text_chunk_size");
  for (auto scalar : scalars)
    textKeysym(scalar);
  std::ostringstream out;
  out.imbue(std::locale::classic()); // XKB syntax must not inherit digit grouping
  out << "xkb_keymap {\nxkb_keycodes \"text-chunk\" { minimum=8; "
         "maximum=255;\n";
  for (size_t i = 0; i < scalars.size(); ++i)
    out << "<T" << i << "> = " << TEXT_KEY_CODES[i] + 8 << ";\n";
  out << "};\nxkb_types \"text\" { type \"ONE_LEVEL\" { modifiers=None; "
         "map[None]=Level1; }; };\n"
         "xkb_compatibility \"text\" {};\nxkb_symbols \"text\" {\n";
  for (size_t i = 0; i < scalars.size(); ++i)
    out << "key <T" << i << "> { type=\"ONE_LEVEL\", [ 0x" << std::hex
        << textKeysym(scalars[i]) << std::dec << " ] };\n";
  out << "};\n};\n";
  return out.str();
}

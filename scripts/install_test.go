package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReleaseInstaller(t *testing.T) {
	for _, tc := range []struct {
		name, mode, arch, version string
		fail                      bool
	}{
		{name: "latest"},
		{name: "explicit", version: "0.1.0"},
		{name: "arm64", arch: "aarch64"},
		{name: "checksum mismatch", mode: "bad-checksum", fail: true},
		{name: "missing checksum", mode: "missing-checksum", fail: true},
		{name: "duplicate checksum", mode: "duplicate-checksum", fail: true},
		{name: "failed download", mode: "download-failure", fail: true},
		{name: "missing binary", mode: "missing-binary", fail: true},
		{name: "unsupported OS", mode: "unsupported", fail: true},
		{name: "unsafe version", version: "../../bad", fail: true},
		{name: "replace symlink", mode: "symlink"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tools := filepath.Join(dir, "tools")
			installDir := filepath.Join(dir, "install with spaces")
			for _, path := range []string{tools, installDir} {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			binary := []byte("#!/bin/sh\necho release-test\n")
			name := "hyprland-computer-use"
			var archive bytes.Buffer
			gz := gzip.NewWriter(&archive)
			tw := tar.NewWriter(gz)
			archiveName := name
			if tc.mode == "missing-binary" {
				archiveName = "unrelated"
			}
			if err := tw.WriteHeader(&tar.Header{Name: archiveName, Mode: 0755, Size: int64(len(binary))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(binary); err != nil {
				t.Fatal(err)
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			arch := "amd64"
			if tc.arch == "aarch64" {
				arch = "arm64"
			}
			asset := fmt.Sprintf("%s_0.1.0_linux_%s.tar.gz", name, arch)
			manifest := fmt.Sprintf("%x  %s\n", sha256.Sum256(archive.Bytes()), asset)
			switch tc.mode {
			case "bad-checksum":
				manifest = strings.Repeat("0", 64) + "  " + asset + "\n"
			case "missing-checksum":
				manifest = ""
			case "duplicate-checksum":
				manifest += manifest
			}
			old := []byte("old executable")
			dest := filepath.Join(installDir, name)
			for path, data := range map[string][]byte{
				filepath.Join(dir, "archive"):   archive.Bytes(),
				filepath.Join(dir, "checksums"): []byte(manifest),
				dest:                            old,
			} {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.mode == "symlink" {
				if err := os.Rename(dest, filepath.Join(dir, "original")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(dir, "original"), dest); err != nil {
					t.Fatal(err)
				}
			}
			writeExecutable(t, filepath.Join(tools, "uname"), `#!/bin/sh
if [ "$1" = -s ]; then
  if [ "$TEST_MODE" = unsupported ]; then echo Darwin; else echo Linux; fi
else
  echo "${TEST_ARCH:-x86_64}"
fi
`)
			writeExecutable(t, filepath.Join(tools, "curl"), `#!/bin/sh
set -eu
out=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2 ;;
    -w) shift 2 ;;
    -fsSL) shift ;;
    https://*) url=$1; shift ;;
    *) exit 90 ;;
  esac
done
printf '%s\n' "$url" >> "$FIXTURE/requests"
[ "$TEST_MODE" != download-failure ] || exit 22
case "$url" in
  https://github.com/samsaffron/hyprland-computer-use/releases/latest)
    printf 'https://github.com/SamSaffron/hyprland-computer-use/releases/tag/v0.1.0' ;;
  "https://github.com/samsaffron/hyprland-computer-use/releases/download/v0.1.0/$TEST_ASSET")
    cp "$FIXTURE/archive" "$out" ;;
  https://github.com/samsaffron/hyprland-computer-use/releases/download/v0.1.0/checksums.txt)
    cp "$FIXTURE/checksums" "$out" ;;
  *) exit 91 ;;
esac
`)
			script, err := filepath.Abs("../install.sh")
			if err != nil {
				t.Fatal(err)
			}
			// Register the script as a Go test-cache input before the shell reads it.
			if _, err := os.ReadFile(script); err != nil {
				t.Fatal(err)
			}
			args := []string{script, "--install-dir", installDir}
			if tc.version != "" {
				args = append(args, "--version", tc.version)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sh", args...)
			cmd.Env = append(os.Environ(), "PATH="+tools+":"+os.Getenv("PATH"), "FIXTURE="+dir,
				"TEST_MODE="+tc.mode, "TEST_ARCH="+tc.arch, "TEST_ASSET="+asset)
			output, err := cmd.CombinedOutput()
			if (err != nil) != tc.fail {
				t.Fatalf("installer: %v\n%s", err, output)
			}
			got, err := os.ReadFile(dest)
			want := binary
			if tc.fail {
				want = old
			}
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("unexpected installed content: %q %v", got, err)
			}
			if !tc.fail {
				info, err := os.Lstat(dest)
				if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0755 {
					t.Fatalf("invalid installed file: %v %v", info, err)
				}
			}
			if tc.mode == "symlink" {
				data, err := os.ReadFile(filepath.Join(dir, "original"))
				if err != nil || !bytes.Equal(data, old) {
					t.Fatal("followed destination symlink")
				}
			}
			if tc.name == "explicit" {
				requests, err := os.ReadFile(filepath.Join(dir, "requests"))
				if err != nil || strings.Contains(string(requests), "/latest") {
					t.Fatal("explicit version should not resolve latest")
				}
			}
			entries, err := os.ReadDir(installDir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("left installation debris: %v %v", entries, err)
			}
		})
	}
}

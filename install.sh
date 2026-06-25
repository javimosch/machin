#!/bin/sh
# Install the latest machin compiler binary from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/javimosch/machin/main/install.sh | sh
#
# Installs to $MACHIN_INSTALL (default ~/.local/bin). machin compiles MFL through C,
# so building programs needs a C compiler (cc/gcc); the wasm target additionally
# needs `zig`. `machin guide` (the language catalog) needs neither.
set -eu

repo="javimosch/machin"
dest="${MACHIN_INSTALL:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "machin: unsupported arch '$arch'" >&2; exit 1 ;;
esac
case "$os" in
  linux|darwin) ;;
  *) echo "machin: unsupported OS '$os' (use the release binaries directly on Windows)" >&2; exit 1 ;;
esac

tag=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" \
        | grep '"tag_name"' | head -1 | cut -d '"' -f 4)
[ -n "$tag" ] || { echo "machin: could not resolve the latest release tag" >&2; exit 1; }

asset="machin-$tag-$os-$arch"
url="https://github.com/$repo/releases/download/$tag/$asset"

echo "machin: downloading $tag ($os/$arch) -> $dest/machin"
mkdir -p "$dest"
curl -fSL --progress-bar "$url" -o "$dest/machin"
chmod +x "$dest/machin"

ver=$("$dest/machin" guide 2>/dev/null | sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' | head -1)
echo "machin: installed $dest/machin ${ver:+(v$ver)}"
case ":$PATH:" in
  *":$dest:"*) echo "machin: run 'machin guide' to learn the language" ;;
  *) echo "machin: add $dest to your PATH, then run 'machin guide'" ;;
esac

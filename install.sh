#!/bin/sh
set -eu

repo="danellalc/localpsp"
bin_dir="${LOCALPSP_INSTALL_DIR:-/usr/local/bin}"

os=$(uname -s)
arch=$(uname -m)

case "$os" in
  Linux) goos=linux ;;
  Darwin) goos=darwin ;;
  *)
    echo "this script only supports Linux and macOS. On Windows, grab the .zip from https://github.com/${repo}/releases/latest" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64 | amd64) goarch=amd64 ;;
  arm64 | aarch64) goarch=arm64 ;;
  *)
    echo "unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

version=$(curl -fsSL "https://api.github.com/repos/${repo}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$version" ]; then
  echo "could not determine the latest localpsp release" >&2
  exit 1
fi

asset="localpsp_${version}_${goos}_${goarch}.tar.gz"
url="https://github.com/${repo}/releases/download/${version}/${asset}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "downloading localpsp ${version} for ${goos}/${goarch}..."
curl -fsSL "$url" -o "$tmp/$asset"
tar -xzf "$tmp/$asset" -C "$tmp"

if [ -w "$bin_dir" ]; then
  mv "$tmp/localpsp" "$bin_dir/localpsp"
else
  echo "no write access to $bin_dir, retrying with sudo..."
  sudo mv "$tmp/localpsp" "$bin_dir/localpsp"
fi
chmod +x "$bin_dir/localpsp"

echo "installed $("$bin_dir/localpsp" version) to $bin_dir/localpsp"

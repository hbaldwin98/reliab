#!/bin/sh
# Installs a reliab release binary, verifying its checksum.
# Usage: reliab-install [version] [dest]
#   version  default: the contents of .reliab-version
#   dest     default: $HOME/.reliab/bin
# Downloads from GitHub Releases over HTTPS, or from the mirror at
# RELIAB_DOWNLOAD_URL, sending RELIAB_TOKEN as a bearer token when set. With
# RELIAB_DOWNLOAD_URL unset, an installed and authenticated `gh` is used
# instead; it is optional. RELIAB_DOWNLOAD_URL may
# also be a local directory or a file:// URL holding the release assets.
# Kept identical to scripts/install.sh; a test enforces it.
set -eu
repo=hbaldwin98/reliab
version=${1:-$(cat .reliab-version 2>/dev/null || true)}
[ -n "$version" ] || { echo "reliab-install: no version given and no .reliab-version" >&2; exit 1; }
version=v${version#v}
dest=${2:-$HOME/.reliab/bin}

case $(uname -s) in
  Linux) os=linux ext= ;;
  Darwin) os=darwin ext= ;;
  MINGW*|MSYS*|CYGWIN*) os=windows ext=.exe ;;
  *) echo "reliab-install: unsupported OS $(uname -s)" >&2; exit 1 ;;
esac
case $(uname -m) in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "reliab-install: unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac
asset="reliab_${version#v}_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fetch() {
  if command -v curl >/dev/null 2>&1; then
    if [ -n "${RELIAB_TOKEN:-}" ]; then
      curl -fsSL -H "Authorization: Bearer $RELIAB_TOKEN" -o "$2" "$1"
    else
      curl -fsSL -o "$2" "$1"
    fi
  elif command -v wget >/dev/null 2>&1; then
    if [ -n "${RELIAB_TOKEN:-}" ]; then
      wget -q --header "Authorization: Bearer $RELIAB_TOKEN" -O "$2" "$1"
    else
      wget -q -O "$2" "$1"
    fi
  else
    echo "reliab-install: need curl, wget, or an authenticated gh" >&2; return 1
  fi
}
if [ -z "${RELIAB_DOWNLOAD_URL:-}" ] && command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  gh release download "$version" -R "$repo" -p "$asset" -p checksums.txt -D "$tmp" ||
    { echo "reliab-install: could not download $asset for release $version from $repo" >&2; exit 1; }
else
  base=${RELIAB_DOWNLOAD_URL:-https://github.com/$repo/releases/download/$version}
  case $base in
    file://*) dir=${base#file://} ;;
    *://*) dir= ;;
    *) dir=$base ;;
  esac
  if [ -n "$dir" ]; then
    cp "$dir/$asset" "$dir/checksums.txt" "$tmp/" ||
      { echo "reliab-install: $asset or checksums.txt is not in $dir" >&2; exit 1; }
  else
    fetch "$base/$asset" "$tmp/$asset"
    fetch "$base/checksums.txt" "$tmp/checksums.txt"
  fi
fi

want=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
  have=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
else
  have=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)
fi
if [ -z "$want" ] || [ "$want" != "$have" ]; then
  echo "reliab-install: checksum mismatch for $asset" >&2; exit 1
fi

(cd "$tmp" && tar -xzf "$asset")
mkdir -p "$dest"
# reliab and the adapters that ship beside it (cmd/adapters).
for bin in "$tmp/reliab$ext" "$tmp"/reliab-adapter-*; do
  [ -f "$bin" ] || continue
  mv "$bin" "$dest/"
  chmod +x "$dest/${bin##*/}"
done
echo "reliab-install: installed reliab $version to $dest" >&2

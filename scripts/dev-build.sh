#!/bin/sh
# Builds reliab and its adapters for local development, where an unreleased
# plugin (0.0.0) looks for them. Usage: scripts/dev-build.sh [dest]
#   dest  default: $HOME/.reliab/dev
set -eu
cd "$(dirname "$0")/.."
dest=${1:-$HOME/.reliab/dev}
ext=
case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) ext=.exe ;; esac
mkdir -p "$dest"
go build -o "$dest/reliab$ext" -ldflags "-X github.com/hbaldwin98/reliab/internal/version.Version=v0.0.0-dev" ./cmd/reliab
for dir in cmd/adapters/*/; do
  name=$(basename "$dir")
  go build -o "$dest/reliab-adapter-$name$ext" "./$dir"
done
echo "dev-build: built reliab and adapters in $dest" >&2

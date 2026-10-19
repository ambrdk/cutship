#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: install.sh <repo> <ref> <dest-dir>" >&2
  exit 2
fi

repo="$1"
ref="$2"
dest="$3"
dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

[[ -n "$ref" ]] || exit 0
tags="$(gh api "repos/$repo/tags" --paginate 2>/dev/null)" || exit 0
tag="$(printf '%s' "$tags" | "$dir/pick_tag.sh" "$ref")"
[[ -n "$tag" ]] || exit 0

mkdir -p "$dest"
if gh release download "$tag" -R "$repo" -p ship_linux_amd64 -D "$dest" --clobber >/dev/null 2>&1; then
  mv "$dest/ship_linux_amd64" "$dest/ship"
  chmod +x "$dest/ship"
  echo "binary=$dest/ship"
  echo "version=$tag"
fi

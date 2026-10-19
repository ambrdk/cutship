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

none() {
  if [[ -n "${CUTSHIP_REQUIRED:-}" ]]; then
    echo "::error::cannot install cutship $ref from $repo: $1" >&2
    exit 1
  fi
  exit 0
}

[[ -n "$ref" ]] || none "no ref"
tags="$(gh api "repos/$repo/tags" --paginate 2>/dev/null)" || none "listing tags failed"
tag="$(printf '%s' "$tags" | "$dir/pick_tag.sh" "$ref")"
[[ -n "$tag" ]] || none "no release tag on that ref"

mkdir -p "$dest"
gh release download "$tag" -R "$repo" -p cutship_linux_amd64 -D "$dest" --clobber >/dev/null 2>&1 || none "release $tag has no cutship_linux_amd64"
mv "$dest/cutship_linux_amd64" "$dest/cutship"
chmod +x "$dest/cutship"
echo "binary=$dest/cutship"
echo "version=$tag"

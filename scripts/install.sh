#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: install.sh <repo> <version-or-ref> <dest-dir>" >&2
  exit 2
fi

repo="$1"
ref="$2"
dest="$3"
dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

fail() {
  echo "::error::cannot install cutship $ref from $repo: $1" >&2
  exit 1
}

[[ -n "$ref" ]] || fail "no version or ref given; pass the version input when cutship runs inside another composite action, where GitHub leaves github.action_ref empty"

for tool in gh jq; do
  command -v "$tool" >/dev/null 2>&1 || fail "$tool is not on PATH; GitHub-hosted runners have it, install it on a self-hosted runner"
done

tmp="$(mktemp -d)"
gh_said() {
  tr '\n' ' ' < "$tmp/gh.err" | sed 's/ *$//'
}

case "${RUNNER_OS:-$(uname -s)}" in
  Linux) os=linux ;;
  macOS | Darwin) os=darwin ;;
  *) fail "unsupported runner OS ${RUNNER_OS:-$(uname -s)}, cutship ships for Linux and macOS" ;;
esac
case "${RUNNER_ARCH:-$(uname -m)}" in
  X64 | x86_64 | amd64) arch=amd64 ;;
  ARM64 | arm64 | aarch64) arch=arm64 ;;
  *) fail "unsupported runner architecture ${RUNNER_ARCH:-$(uname -m)}" ;;
esac

tags="$(gh api "repos/$repo/tags" --paginate 2>"$tmp/gh.err")" || fail "listing tags failed: $(gh_said)"
tag="$(printf '%s' "$tags" | "$dir/pick_tag.sh" "$ref")"
[[ -n "$tag" ]] || fail "no release tag vX.Y.Z on that ref; pin the action to the commit of a release or set the version input"

archive="cutship_${tag#v}_${os}_${arch}.tar.gz"
gh release download "$tag" -R "$repo" -p "$archive" -p checksums.txt -D "$tmp" --clobber >/dev/null 2>"$tmp/gh.err" ||
  fail "release $tag has no $archive or checksums.txt: $(gh_said)"

want="$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[[ -n "$want" ]] || fail "$archive is not in checksums.txt of $tag"
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/$archive" | awk '{ print $1 }')"
else
  got="$(shasum -a 256 "$tmp/$archive" | awk '{ print $1 }')"
fi
[[ "$got" == "$want" ]] || fail "checksum mismatch for $archive: got $got, checksums.txt says $want"

mkdir -p "$dest"
tar -xzf "$tmp/$archive" -C "$dest" cutship
chmod +x "$dest/cutship"
echo "path=$dest"
echo "binary=$dest/cutship"
echo "version=$tag"

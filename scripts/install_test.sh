#!/usr/bin/env bash
set -uo pipefail

dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
failed=0

mkdir -p "$work/bin"
cat > "$work/bin/gh" <<'GH'
#!/usr/bin/env bash
if [[ "$1" == api ]]; then
  printf '%s' "$FAKE_TAGS"
  exit 0
fi
if [[ "$1 $2" == "release download" ]]; then
  tag="$3"
  shift 3
  patterns=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -D) dest="$2"; shift 2 ;;
      -p) patterns+=("$2"); shift 2 ;;
      *) shift ;;
    esac
  done
  if [[ " $FAKE_ASSETS " != *" $tag "* ]]; then
    echo "no assets match the file pattern" >&2
    exit 1
  fi
  mkdir -p "$dest"
  printf '%s\n' "${patterns[@]}" >> "$FAKE_LOG"
  for p in "${patterns[@]}"; do
    [[ "$p" == *.tar.gz ]] || continue
    src="$(mktemp -d)"
    printf '#!/bin/sh\necho "cutship %s"\n' "$tag" > "$src/cutship"
    chmod +x "$src/cutship"
    tar -czf "$dest/$p" -C "$src" cutship
    archive="$p"
  done
  for p in "${patterns[@]}"; do
    [[ "$p" == checksums.txt ]] || continue
    sum="$(shasum -a 256 "$dest/$archive" | awk '{print $1}')"
    if [[ -n "${FAKE_BAD_SUM:-}" ]]; then
      sum="0000000000000000000000000000000000000000000000000000000000000000"
    fi
    name="$archive"
    if [[ -n "${FAKE_NO_SUM_LINE:-}" ]]; then
      name="cutship_other.tar.gz"
    fi
    printf '%s  %s\n' "$sum" "$name" > "$dest/checksums.txt"
  done
  exit 0
fi
exit 1
GH
chmod +x "$work/bin/gh"

tags='[
  {"name":"v1","commit":{"sha":"ccc"}},
  {"name":"v0.3.0","commit":{"sha":"ccc"}},
  {"name":"v0.2.0","commit":{"sha":"bbb"}}
]'

install() {
  local name="$1" ref="$2" os="$3" arch="$4"
  shift 4
  env PATH="$work/bin:$PATH" FAKE_TAGS="$tags" FAKE_ASSETS="v0.3.0" FAKE_LOG="$work/$name.log" RUNNER_OS="$os" RUNNER_ARCH="$arch" "$@" \
    "$dir/install.sh" ambrdk/cutship "$ref" "$work/$name" 2>&1
}

fail() {
  echo "FAIL: $1"
  failed=$((failed + 1))
}

test_install_linux_amd64() {
  local out
  out="$(install linux ccc Linux X64)" || fail "linux: exit $? ($out)"
  [[ "$out" == "path=$work/linux
binary=$work/linux/cutship
version=v0.3.0" ]] || fail "linux: output '$out'"
  [[ "$("$work/linux/cutship")" == "cutship v0.3.0" ]] || fail "linux: binary does not run"
  grep -qx "cutship_0.3.0_linux_amd64.tar.gz" "$work/linux.log" || fail "linux: asked for $(cat "$work/linux.log")"
}

test_arch_mapping() {
  install mac v0.3.0 macOS ARM64 > /dev/null || fail "mac: exit $?"
  grep -qx "cutship_0.3.0_darwin_arm64.tar.gz" "$work/mac.log" || fail "mac: asked for $(cat "$work/mac.log")"
  install arm v0.3.0 Linux ARM64 > /dev/null || fail "arm: exit $?"
  grep -qx "cutship_0.3.0_linux_arm64.tar.gz" "$work/arm.log" || fail "arm: asked for $(cat "$work/arm.log")"
}

expect_error() {
  local name="$1" text="$2" out code
  shift 2
  out="$("$@")"
  code=$?
  [[ "$code" == 1 ]] || fail "$name: exit $code ($out)"
  [[ "$out" == *"::error::"*"$text"* ]] || fail "$name: no error with '$text' in '$out'"
  [[ ! -e "$work/$name/cutship" ]] || fail "$name: binary installed"
}

test_checksum_mismatch() {
  expect_error mismatch "checksum mismatch" install mismatch v0.3.0 Linux X64 FAKE_BAD_SUM=1
}

test_missing_checksum() {
  expect_error nosum "not in checksums.txt" install nosum v0.3.0 Linux X64 FAKE_NO_SUM_LINE=1
}

test_no_release() {
  expect_error norelease "no release tag" install norelease ddd Linux X64
  expect_error noasset "has no" install noasset bbb Linux X64
  expect_error unknown "no release tag" install unknown v9.9.9 Linux X64
  expect_error empty "no version" install empty "" Linux X64
  expect_error windows "unsupported" install windows v0.3.0 Windows X64
}

test_explicit_version() {
  local out
  out="$(install moving v1 Linux X64)" || fail "moving: exit $?"
  [[ "$out" == *"version=v0.3.0" ]] || fail "moving: '$out'"
}

test_install_linux_amd64
test_arch_mapping
test_checksum_mismatch
test_missing_checksum
test_no_release
test_explicit_version

echo "$failed failed"
[[ "$failed" -eq 0 ]]

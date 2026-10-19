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
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -D) dest="$2"; shift 2 ;;
      *) shift ;;
    esac
  done
  if [[ " $FAKE_ASSETS " == *" $tag "* ]]; then
    printf 'binary %s' "$tag" > "$dest/ship_linux_amd64"
    exit 0
  fi
  echo "no assets match the file pattern" >&2
  exit 1
fi
exit 1
GH
chmod +x "$work/bin/gh"

tags='[
  {"name":"v1","commit":{"sha":"ccc"}},
  {"name":"v0.3.0","commit":{"sha":"ccc"}},
  {"name":"v0.2.0","commit":{"sha":"bbb"}}
]'

check() {
  local name="$1" ref="$2" want="$3"
  local dest="$work/$name" out
  out="$(PATH="$work/bin:$PATH" FAKE_TAGS="$tags" FAKE_ASSETS="v0.3.0" "$dir/install.sh" ambrdk/ship "$ref" "$dest" 2>&1)"
  if [[ "$out" != "$want" ]]; then
    echo "FAIL: $name: got '$out', want '$want'"
    failed=$((failed + 1))
    return
  fi
  if [[ -n "$want" && ! -x "$dest/ship" ]]; then
    echo "FAIL: $name: $dest/ship is not executable"
    failed=$((failed + 1))
  fi
}

check_required() {
  local name="$1" ref="$2" want_code="$3"
  local out code
  out="$(PATH="$work/bin:$PATH" FAKE_TAGS="$tags" FAKE_ASSETS="v0.3.0" SHIP_REQUIRED=1 "$dir/install.sh" ambrdk/ship "$ref" "$work/$name" 2>&1)"
  code=$?
  if [[ "$code" != "$want_code" ]]; then
    echo "FAIL: $name: exit $code, want $want_code ($out)"
    failed=$((failed + 1))
    return
  fi
  if [[ "$want_code" != 0 && "$out" != *"::error::"* ]]; then
    echo "FAIL: $name: no ::error:: in '$out'"
    failed=$((failed + 1))
  fi
}

check_required "required release" v0.3.0 0
check_required "required release without asset" v0.2.0 1
check_required "required unknown version" v9.9.9 1

check "release with asset" ccc "binary=$work/release with asset/ship
version=v0.3.0"
check "moving tag" v1 "binary=$work/moving tag/ship
version=v0.3.0"
check "old release without ship asset" bbb ""
check "commit without release" ddd ""
check "branch" main ""
check "empty ref" "" ""

echo "$failed failed"
[[ "$failed" -eq 0 ]]

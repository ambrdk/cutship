#!/usr/bin/env bash
set -uo pipefail

dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
failed=0

check() {
  local name="$1" want="$2" sha="$3" tags="$4"
  local out code
  out="$(printf '%s' "$tags" | "$dir/pick_tag.sh" "$sha" 2>&1)"
  code=$?
  if [[ "$code" != 0 || "$out" != "$want" ]]; then
    echo "FAIL: $name: got '$out' (exit $code), want '$want'"
    failed=$((failed + 1))
  fi
}

tags='[
  {"name":"v1","commit":{"sha":"aaa"}},
  {"name":"v0.1.1","commit":{"sha":"aaa"}},
  {"name":"v0.1.10","commit":{"sha":"aaa"}},
  {"name":"v0.1.2","commit":{"sha":"bbb"}},
  {"name":"v0.2.0","commit":{"sha":"ccc"}}
]'

check "highest semver on sha" v0.1.10 aaa "$tags"
check "single semver" v0.1.2 bbb "$tags"
check "only moving tag" "" ddd '[{"name":"v1","commit":{"sha":"ddd"}}]'
check "unknown sha" "" eee "$tags"
check "empty list" "" aaa '[]'
check "two pages" v0.3.0 fff '[{"name":"v0.2.9","commit":{"sha":"fff"}}][{"name":"v0.3.0","commit":{"sha":"fff"}}]'
check "pinned by release tag" v0.1.2 v0.1.2 "$tags"
check "pinned by moving tag" v0.1.10 v1 "$tags"
check "pinned by branch" "" main "$tags"
check "moving tag on another page" v0.3.0 v1 '[{"name":"v0.3.0","commit":{"sha":"fff"}}][{"name":"v1","commit":{"sha":"fff"}}]'

echo "$failed failed"
[[ "$failed" == 0 ]]

#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: pick_tag.sh <sha-or-tag>" >&2
  exit 2
fi

jq -rs --arg ref "$1" '
  (add // []) as $tags
  | ([$tags[] | select(.name == $ref) | .commit.sha][0] // $ref) as $sha
  | $tags[]
  | select(.commit.sha == $sha and (.name | test("^v[0-9]+\\.[0-9]+\\.[0-9]+$")))
  | .name' |
  sort -V | tail -n 1

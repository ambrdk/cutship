#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: pick_tag.sh <sha>" >&2
  exit 2
fi

jq -rs --arg sha "$1" 'add // [] | .[] | select(.commit.sha == $sha and (.name | test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))) | .name' |
  sort -V | tail -n 1

#!/usr/bin/env bash
# Fetch one golden PR's metadata + diff into testdata/golden/<name>/.
# Usage: scripts/fetch-golden.sh <name> <workspace> <repo> <pr-id>
# .env must be plain unquoted KEY=value lines (no export), matching loadDotEnv.
set -euo pipefail
cd "$(dirname "$0")/.."
name=${1:?usage: fetch-golden.sh <name> <workspace> <repo> <pr-id>} ws=${2:?} repo=${3:?} id=${4:?}
if [ -f ./.env ]; then set -a; source ./.env; set +a; fi
: "${BITBUCKET_EMAIL:?set BITBUCKET_EMAIL}" "${BITBUCKET_API_TOKEN:?set BITBUCKET_API_TOKEN}"
dir="testdata/golden/$name"
mkdir -p "$dir"
curl -fsS --remove-on-error -u "$BITBUCKET_EMAIL:$BITBUCKET_API_TOKEN" \
  "https://api.bitbucket.org/2.0/repositories/$ws/$repo/pullrequests/$id" -o "$dir/pr.json"
curl -fsSL --remove-on-error -u "$BITBUCKET_EMAIL:$BITBUCKET_API_TOKEN" \
  "https://api.bitbucket.org/2.0/repositories/$ws/$repo/pullrequests/$id/diff" -o "$dir/diff.txt"
echo "saved $dir ($(grep -c '^diff --git' "$dir/diff.txt") files, $(wc -l < "$dir/diff.txt") lines)"

#!/usr/bin/bash

die() {
    printf "%s\n" "$*"
    exit 1
}

set -e

test $# -gt 0 || die "Usage: $0 CMD..."

cd "$(dirname "$0")/.."

DIR="$(mktemp -t -d dpu-operator-check-gittree-for-diff.XXXX)"

cp -ar ./ "$DIR/"

echo "Checking \`$@\` in \"$DIR\""

pushd "$DIR/" 1>/dev/null
"$@"
popd 1>/dev/null

diff -r . "$DIR/" || die "There is a difference between \"$PWD\" and \"$DIR\" after calling \`$@\`"

rm -rf "$DIR/"

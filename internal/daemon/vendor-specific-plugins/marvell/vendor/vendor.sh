#!/usr/bin/env bash

set -ex

OCTEON_GITSHA="35c9be07d2eefe1c909efefc9faa495db965a58e"

die() {
    printf "%s\n" "$*"
    exit 1
}

_sha() {
    local FILE="$1"
    local SHA

    SHA="$(sha256sum "$FILE")"
    printf '%s' "${SHA%% *}"
}

vendor_pcie_ep_octeon_target() {
    local REPO="https://github.com/MarvellEmbeddedProcessors/pcie_ep_octeon_target.git"
    local DIR="pcie_ep_octeon_target"

    rm -rf "$DIR/"

    git clone "$REPO"

    cd "$DIR"

    git checkout "$OCTEON_GITSHA"

    rm -rf ".git/"
    rm -rf ".github/"
}

BASEDIR="$(dirname "$0")"
cd "$BASEDIR"

vendor_pcie_ep_octeon_target

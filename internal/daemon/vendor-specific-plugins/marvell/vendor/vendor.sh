#!/usr/bin/env bash

set -ex

OCTEON_GITSHA="aa84a2331f76b68583e7b5861f17f5f3cef0fbd0"

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

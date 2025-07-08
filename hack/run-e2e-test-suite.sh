#!/bin/bash

set -e

die() {
    printf '%s\n' "$*"
    exit 1
}

cd "$(dirname "$0")/.." || die "failure to change directory for $0"

export FAST_TEST="${FAST_TEST:-true}"
export REGISTRY="${REGISTRY:-$(hostname | sed 's/$/:5000/')}"
export NF_INGRESS_IP="${NF_INGRESS_IP:-10.20.30.2}"
export EXTERNAL_CLIENT_DEV="${EXTERNAL_CLIENT_DEV:-eno12409}"
export EXTERNAL_CLIENT_IP="${EXTERNAL_CLIENT_IP:-10.20.30.100}"
export BINDIR="${BINDIR:-bin}"
export ENVTEST_K8S_VERSION="${ENVTEST_K8S_VERSION:-$(sed -n 's/^ *ENVTEST_K8S_VERSION: \+\(.*\)$/\1/p' taskfile.yaml)}"

BINDIR_ABS="$BINDIR"
if [[ "$BINDIR_ABS" != /* ]] ; then
    BINDIR_ABS="$PWD/$BINDIR"
fi

GINKO_ARGS=()

if [ -n "$TEST_FOCUS" ] ; then
    GINKO_ARGS+=( "-focus=${TEST_FOCUS}" )
fi

KUBEBUILDER_ASSETS="$( "$BINDIR/setup-envtest" use "$ENVTEST_K8S_VERSION" --bin-dir "$BINDIR_ABS" -p path )"
export KUBEBUILDER_ASSETS

run_test() {
    "$BINDIR/ginkgo" -coverprofile cover.out "${GINKO_ARGS[@]}" ./e2e_test/...
}

rc=0
run_test || rc="$?"

if [ "$rc" != 0 ] ; then
    echo ">>> Test failed. Rerun a few times!!"
    for i in {1..5} ; do
        echo ">>> Re-Run #$i..."
        rc2=0
        run_test || rc2=0
        echo ">>> Re-Run #$i completed: result $rc2"
    done
fi

exit "$rc"

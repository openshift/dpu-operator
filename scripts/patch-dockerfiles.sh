#!/bin/bash

set -e

NAMES=(
    dpu-operator-build-builder
    dpu-operator-build-base
)

die() {
    printf "%s\n" "$*"
    exit 1
}

_buildah_manifest_rm() {
    buildah manifest rm "$1" &>/dev/null || :
}

local_containers_clean() {
    local name

    for name in "${NAMES[@]}" ; do
        _buildah_manifest_rm "$name"
    done
}

local_container_build() {
    local name="$1"

    buildah manifest exists "$name" && return 0

    buildah manifest create "$name" || return 1

    local rc0=0
    buildah build --layers --manifest "$name" --platform linux/amd64,linux/arm64 -f - \
        || rc0="$?"

    if [ "$rc0" -ne 0 ] ; then
        _buildah_manifest_rm "$name"
    fi

    return "$rc0"
}

local_container_build_all() {

    cat <<EOF | local_container_build dpu-operator-build-builder
FROM quay.io/centos/centos:stream9
RUN dnf install -y \\
  golang \\
  hostname \\
  make \\
  https://kojihub.stream.centos.org/kojifiles/vol/koji02/packages/golang/1.23.6/1.el10/\$(uname -m)/golang-1.23.6-1.el10.\$(uname -m).rpm \\
  https://kojihub.stream.centos.org/kojifiles/vol/koji02/packages/golang/1.23.6/1.el10/\$(uname -m)/golang-bin-1.23.6-1.el10.\$(uname -m).rpm \\
  https://kojihub.stream.centos.org/kojifiles/vol/koji02/packages/golang/1.23.6/1.el10/noarch/golang-src-1.23.6-1.el10.noarch.rpm
ENV GOCACHE=/go/.cache
EOF

    cat <<EOF | local_container_build dpu-operator-build-base
FROM quay.io/centos/centos:stream9
EOF

}

do_build() {
   local_container_build_all
}

do_patch() {
    local file="$1"

    PATCH_DOCKERFILES_INTELVSP="${PATCH_DOCKERFILES_INTELVSP:-https://file.corp.redhat.com/~thaller/dpu-operator/patch-dockerfiles-IntelVSP.sh}"

    cat "$file" | \
    \
    sed \
        -e 's#^FROM registry.ci.openshift.org/[^ ]* AS builder$#FROM localhost/dpu-operator-build-builder AS builder#' \
        -e 's#^FROM registry.ci.openshift.org/[^ ]*$#FROM localhost/dpu-operator-build-base#' | \
    \
    if [ "${file##*/}" = "Dockerfile.IntelVSP.rhel" ] ; then
        # We need openvswitch3.4, which is only accessible from inside Red Hat.
        # Patch in a RUN command with a curl-to-bash script what works around
        # this.
        #
        # Theoretically, you can overwrite PATCH_DOCKERFILES_INTELVSP, but it's
        # not clear where you get a working openvswitch3.4 version without
        # access to Red Hat internals.
        sed -e 's#^ENV PYTHONUNBUFFERED=1$#\0\n\nRUN curl -sk "'"$PATCH_DOCKERFILES_INTELVSP"'" | bash -#'
    else
        cat
    fi
}

if [ "$1" = "patch" ] ; then
    if [ "$#" -ne 3 ] ; then
        die "The patch command requires 2 arguments (the input file and the output file)"
    fi
elif [ "$#" -ne 1 ] ; then
    die "Requires command type as first argument. Must be one of [clean,build,build-force,patch]"
fi

case "$1" in
    "clean")
        local_containers_clean
        ;;
    "build")
        do_build
        ;;
    "build-force")
        local_containers_clean
        do_build
        ;;
    "patch")
        do_build
        do_patch "$2" > "$3"
        ;;
    *)
        die "Invalid argument. Must be one of [clean,build,build-force,patch]"
        ;;
esac

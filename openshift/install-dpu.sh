#!/bin/bash

set -euo pipefail

PIP_OPTS="--no-cache-dir"
# Check if we are building the image in the OSBS environment. If so, source the
# env vars specific for enabling cachito.
if [ -d ${REMOTE_SOURCES_DIR}/cachito-gomod-with-deps ]; then
    source ${REMOTE_SOURCES_DIR}/cachito-gomod-with-deps/cachito.env
    cd ${REMOTE_SOURCES_DIR}/cachito-gomod-with-deps/app/openshift
    export GRPC_PYTHON_BUILD_EXT_COMPILER_JOBS=16
else
    cd  ${REMOTE_SOURCES_DIR}
fi

python3 -m pip install --upgrade pip

# Install the packages in order of build dependency to avoid issues during installation.
python3 -m pip install ${PIP_OPTS} -r requirements-build.txt
python3 -m pip install ${PIP_OPTS} -r requirements.txt

rm -rf ${REMOTE_SOURCES_DIR}

#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Copyright (C) 2021 Marvell.

set -e

PROJECT_ROOT=${PROJECT_ROOT:-$PWD}
cd $PROJECT_ROOT

echo "Running checkformat script over patch"

$PROJECT_ROOT/ci/checkpatch/run_checkformat.sh 

echo "Running checkpatch script over patch"

$PROJECT_ROOT/ci/checkpatch/run_checkpatch.sh

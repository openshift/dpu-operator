#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Copyright (C) 2021 Marvell.

set -e

ERR=
PROJECT_ROOT=${PROJECT_ROOT:-$PWD}
cd $PROJECT_ROOT
export PCIEP_CHECKPATCH_CODESPELL=$PROJECT_ROOT/ci/checkpatch/dictionary.txt
export PCIEP_CHECKPATCH_PATH=$PROJECT_ROOT/ci/checkpatch/checkpatch.pl
git format-patch -n1 -q -o patches
./ci/checkpatch/devtools/checkpatches.sh patches/* || ERR=1

rm -rf patches

if [ -n "$ERR" ]; then
	echo "Checkpatch / git log check failed !!!"
	exit 1
fi


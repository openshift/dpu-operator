#!/bin/bash
#Copyright (C) 2023 Intel Corporation
#SPDX-License-Identifier: Apache-2.0

set -e
GOFMTOUT=$(find . -type f -name '*.go' -not -path "./sriov_cni/*"  -print0 | xargs -0 gofmt -d 2>&1);
if [[ -z $GOFMTOUT ]]; then
    exit 0;
else
    echo "$GOFMTOUT"
    echo ""
    echo "gomft found formatting issues above ^^^ "
    echo "Please run go fmt ./... to format your go source files."
    echo ""
    exit 1;
fi;

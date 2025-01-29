#!/usr/bin/env bash

set -e

cd cluster-deployment-automation
source /tmp/cda-venv/bin/activate

./cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu-host.yaml deploy -s post

ret=$?
if [ $ret == 0 ]; then
    echo "Successfully Deployed DPU host Cluster's post config"
else
    echo "cluster-deployment-automation deployment post config failed with error code $ret"
    exit $ret
fi

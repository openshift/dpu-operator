#!/usr/bin/env bash

set -e

cd cluster-deployment-automation
source /tmp/cda-venv/bin/activate

./cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu.yaml deploy -s post

ret=$?
if [ $ret == 0 ]; then
    echo "Successfully Deployed ISO cluster's post config"
else
    echo "cluster-deployment-automation post config deployment failed with error code $ret"
    exit $ret
fi

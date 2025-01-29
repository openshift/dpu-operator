#!/usr/bin/env bash

set -e

cd cluster-deployment-automation
source /tmp/cda-venv/bin/activate

# Tear down any previous cluster fully
./cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu-host.yaml deploy -f

./cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu.yaml deploy

ret=$?
if [ $ret == 0 ]; then
    echo "Successfully Deployed ISO Cluster"
else
    echo "cluster-deployment-automation deployment failed with error code $ret"
    exit $ret
fi

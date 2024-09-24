#!/usr/bin/env bash

set -e

python3.11 -m venv ./.tmp/ocp-venv

source ./.tmp/ocp-venv/bin/activate

cd cluster-deployment-automation
python cda.py --secret /root/pull_secret.json ../cluster_configs/config-dpu.yaml deploy

ret=$?
if [ $ret == 0 ]; then
    echo "Successfully Deployed ISO Cluster"
else
    echo "cluster-deployment-automation deployment failed with error code $ret"
    exit $ret
fi

#!/usr/bin/env bash

cd cluster-deployment-automation
python3.11 -m venv /tmp/cda-venv
source /tmp/cda-venv/bin/activate

python3.11 cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu.yaml deploy -s post

ret=$?
if [ $ret == 0 ]; then
    echo "Successfully Deployed ISO cluster's post config"
else
    echo "cluster-deployment-automation post config deployment failed with error code $ret"
    exit $ret
fi

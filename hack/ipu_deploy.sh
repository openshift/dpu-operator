#!/bin/bash
cd cluster-deployment-automation
source ocp-venv/bin/activate

python3.11 cda.py ../cluster_configs/config-dpu.yaml deploy

ret=$?
if [ $ret == 0 ]; then
    echo "Successfully Deployed ISO Cluster"
else
    echo "cluster-deployment-automation deployment failed with error code $ret"
    exit $ret
fi


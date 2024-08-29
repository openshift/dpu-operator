#!/bin/bash
cd cluster-deployment-automation
source ocp-venv/bin/activate

# We need to overwrite the buildID to ensure that Jenkin's ProcessTreeKiller does not clean up the containers spawned by CDA on job completion. https://wiki.jenkins.io/display/JENKINS/ProcessTreeKiller
# Specifically, we want the local container registry to persist so we do not need to rebuild images each time we run a new job.
export BUILD_ID=dontKillMe

current_pwd=$(pwd)

path=${current_pwd%/cluster-deployment-automation}

export path

envsubst < ../cluster_configs/config-dpu.yaml > config-dpu.yaml

python3.11 cda.py --secret /root/pull_secret.json config-dpu.yaml deploy

ret=$?
if [ $ret == 0 ]; then
    echo "Successfully Deployed ISO Cluster"
else
    echo "cluster-deployment-automation deployment failed with error code $ret"
    exit $ret
fi


cd cluster-deployment-automation
python3.11 -m venv /tmp/ocp-venv
source /tmp/ocp-venv/bin/activate

export PATH=$PATH:"/usr/local/go/bin"

# We need to overwrite the buildID to ensure that Jenkin's ProcessTreeKiller does not clean up the containers spawned by CDA on job completion. https://wiki.jenkins.io/display/JENKINS/ProcessTreeKiller
# Specifically, we want the local container registry to persist so we do not need to rebuild images each time we run a new job.
export BUILD_ID=dontKillMe

current_pwd=$(pwd)

path=${current_pwd%/cluster-deployment-automation}

python3.11 cda.py --secret /root/pull_secret.json ../cluster_configs/config-dpu-host.yaml deploy 

ret=$?
if [ $ret == 0 ]; then
    echo "Successfully Deployed DPU host Cluster"
else
    echo "cluster-deployment-automation deployment failed with error code $ret"
    exit $ret
fi


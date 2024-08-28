cd cluster-deployment-automation
source ocp-venv/bin/activate

export PATH=$PATH:"/usr/local/go/bin"

# We need to overwrite the buildID to ensure that Jenkin's ProcessTreeKiller does not clean up the containers spawned by CDA on job completion. https://wiki.jenkins.io/display/JENKINS/ProcessTreeKiller
# Specifically, we want the local container registry to persist so we do not need to rebuild images each time we run a new job.
export BUILD_ID=dontKillMe

if [ -z "$pullnumber" ]; then
    json_blob=$(curl https://api.github.com/repos/openshift/dpu-operator/pulls/$pullnumber)
    branch=$(echo $json_blob | jq -r '.head.ref')
else
    branch="main"
fi

python3.11 cda.py ../cluster_configs/config-dpu-host.yaml deploy

ret=$?
if [ $ret == 0 ]; then
    echo "Successfully Deployed DPU host Cluster"
else
    echo "cluster-deployment-automation deployment failed with error code $ret"
    exit $ret
fi


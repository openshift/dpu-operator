#!/usr/bin/env bash

set -e

cd cluster-deployment-automation
source /tmp/cda-venv/bin/activate

# Tear down any previous cluster fully
./cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu-host.yaml deploy -f

parallel -u --halt 2 ::: \
  "./cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu.yaml deploy && echo 'Successfully Deployed ISO Cluster'" \
  "./cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu-host.yaml deploy --steps pre,masters && echo 'Successfully Deployed DPU host Cluster'"

./cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu-host.yaml deploy --steps workers,post && echo "Successfully Deployed DPU host Cluster"

# WA to avoid starting the e2e-tests before CRDs are fully installed
sleep 60

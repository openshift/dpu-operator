#!/usr/bin/env bash

set -e

cd cluster-deployment-automation
python3.11 -m venv /tmp/cda-venv
source /tmp/cda-venv/bin/activate

# Tear down any previous cluster fully
python3.11 cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu-host.yaml deploy -f

parallel -u --halt 2 ::: \
  "python3.11 cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu.yaml deploy && echo 'Successfully Deployed ISO Cluster'" \
  "python3.11 cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu-host.yaml deploy --steps pre,masters && echo 'Successfully Deployed DPU host Cluster'"

python3.11 cda.py --secret /root/pull_secret.json ../hack/cluster-configs/config-dpu-host.yaml deploy --steps workers,post && echo "Successfully Deployed DPU host Cluster"

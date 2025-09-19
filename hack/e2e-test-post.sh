#!/usr/bin/env bash

set -ex

if [ "$JOB_NAME" != "99_E2E_Marvell_DPU_Deploy" ] ; then
    echo "Nothing to do for $0 with JOB_NAME $JOB_NAME"
    exit 0
fi

export KUBECONFIG=/root/kubeconfig.ocpcluster

oc debug -t node/worker-40 -- nsenter -a -t 1 bash -c 'reboot'
ssh root@172.16.3.16 reboot

sleep 120

oc wait --for=condition=Ready node/worker-40 --timeout=300s

#!/usr/bin/env bash

set -e

git submodule init
git submodule update

systemctl restart libvirtd

rm -rf ./.tmp/ocp-venv
python3.11 -m venv ./.tmp/ocp-venv

source ./.tmp/ocp-venv/bin/activate

pushd cluster-deployment-automation
sh ./dependencies.sh
popd

pushd ocp-traffic-flow-tests
pip install -r requirements.txt
popd

#!/usr/bin/env bash

git submodule init
git submodule update

cd cluster-deployment-automation
rm -rf /tmp/cda-venv
python3.11 -m venv /tmp/cda-venv
source /tmp/cda-venv/bin/activate
sh ./dependencies.sh
deactivate
cd -

cd ocp-traffic-flow-tests
rm -rf /tmp/tft-venv
python3.11 -m venv /tmp/tft-venv
source /tmp/tft-venv/bin/activate
pip3.11 install -r requirements.txt
deactivate
cd -

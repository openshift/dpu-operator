#!/usr/bin/env bash

set -e

setup_venv() {
    local submodule_dir=$1
    local venv_path=$2
    local setup_script=$3
    local sha_file=$venv_path/.submodule_sha

    cd $submodule_dir

    current_sha=$(git rev-parse HEAD)

    if [[ -d $venv_path && -f $sha_file && $(cat $sha_file) == "$current_sha" ]]; then
        echo "Virtual environment for $submodule_dir is up to date. Skipping setup."
    else
        echo "Setting up virtual environment for $submodule_dir..."
        rm -rf $venv_path
        python3.11 -m venv $venv_path
        source $venv_path/bin/activate
        eval $setup_script
        echo $current_sha > $sha_file
        deactivate
    fi

    cd -
}

setup_venv cluster-deployment-automation /tmp/cda-venv "sh ./dependencies.sh"
setup_venv kubernetes-traffic-flow-tests /tmp/tft-venv "pip install -r requirements.txt"

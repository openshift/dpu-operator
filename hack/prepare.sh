rm -rf cluster-deployment-automation

CDA_REPO=${CDA_REPO:-https://github.com/bn222/cluster-deployment-automation.git}
CDA_BRANCH=${CDA_BRANCH:-main}

git submodule init
git submodule update

cd cluster-deployment-automation
git checkout "$CDA_BRANCH"

systemctl restart libvirtd

python3.11 -m venv /tmp/ocp-venv
source /tmp/ocp-venv/bin/activate
sh ./dependencies.sh


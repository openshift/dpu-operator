#!/bin/sh
set -eux
#Note, set pipefail supported in bash, but not in sh, so below is the alternative to check failures when using pipe(with /bin/sh)
set -o errexit 

output="$(pip show pip | grep Location)" && \
pkg_install_path="$(echo "$output" | awk '{print $2}')" && \
echo "$pkg_install_path" && \
cp -R /opt/p4/p4-cp-nws/bin/p4 "$pkg_install_path"

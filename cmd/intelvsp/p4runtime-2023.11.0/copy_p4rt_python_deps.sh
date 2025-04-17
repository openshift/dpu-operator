#!/bin/sh
set -eux
#Note, set pipefail supported in bash, but not in sh, so below is the alternative to check failures when using pipe(with /bin/sh)
set -o errexit 

output="$(pip show pip | grep Location)" && \
pkg_install_path="$(echo "$output" | awk '{print $2}')" && \
echo "$pkg_install_path" && \
mkdir -p $pkg_install_path/p4 && \
cp -R /opt/p4rt_proto/* "$pkg_install_path"/p4/

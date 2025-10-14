
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2024 Intel Corporation

FROM registry.access.redhat.com/ubi9/ubi:9.3-1610

ARG P4_NAME
ENV P4_NAME $P4_NAME

ARG TARGETOS
ARG TARGETARCH
ENV ARCHSUFFIX="aarch64"

COPY . /src
WORKDIR /src
RUN mkdir -pv /opt/ && tar -zxvf p4.tar.gz -C /opt/ && rm -f p4.tar.gz

COPY p4 /opt/p4/${P4_NAME}
COPY images/entrypoint.sh /
COPY images/es2k_skip_p4.conf.template /
RUN dnf -y install   kmod-28-10.el9 \
      gettext-0.21-8.el9 \
      python3-pip-21.3.1-1.el9 \
      pciutils-3.7.0-5.el9 \
      libnl3-3.9.0-1.el9 \
      libedit-3.1-38.20210216cvs.el9 \
      net-tools-2.0-0.64.20160912git.el9 \
      libatomic-11.5.0-2.el9 \
      iputils-20210202-9.el9 \
      && dnf clean all

RUN if [ "$TARGETARCH" = "amd64" ]; then export ARCHSUFFIX="x86_64"; \
    else export ARCHSUFFIX="aarch64"; fi; \
    curl -L -o libconfig-1.7.2-9.el9.$ARCHSUFFIX.rpm \
     https://mirror.stream.centos.org/9-stream/BaseOS/$ARCHSUFFIX/os/Packages/libconfig-1.7.2-9.el9.$ARCHSUFFIX.rpm \
    && dnf -y install libconfig-1.7.2-9.el9.$ARCHSUFFIX.rpm && rm -f libconfig-1.7.2-9.el9.$ARCHSUFFIX.rpm && dnf clean all

RUN python3 -m pip install --no-cache-dir \
    netaddr==1.2.1 \
    /opt/p4/p4-cp-nws/share/stratum/p4runtime/p4runtime-2024.8.0-py3-none-any.whl

WORKDIR /

ENTRYPOINT ["/entrypoint.sh"]

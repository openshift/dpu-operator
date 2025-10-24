# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2024 Intel Corporation
# Specify the base image
FROM gcc:latest@sha256:4301d48153d6bd1307c820bb74a4463dfbc9a9c1ad7f5b8426f62d0d6c02347c
# Copy the program files
COPY nf.c /app/
# Set the working directory
WORKDIR /app
# Compile the program  
RUN gcc -o nf nf.c && chmod +x nf
# Set the entry point
CMD ["./nf"]
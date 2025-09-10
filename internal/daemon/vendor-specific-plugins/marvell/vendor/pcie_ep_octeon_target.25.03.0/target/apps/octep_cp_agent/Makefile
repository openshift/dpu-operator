#SPDX-License-Identifier: BSD-3-Clause
#Copyright (c) 2022 Marvell.

PLAT ?= aarch64

ifeq ($(INSTALL_PATH),)
INSTALL_PATH=$(CURDIR)/bin
endif

CC ?= $(CROSS_COMPILE)gcc
LD ?= $(CROSS_COMPILE)ld

NAME_PREFIX=octep_cp
APP_NAME=$(NAME_PREFIX)_agent

SRCS = main.c loop.c app_config.c
APP_CFLAGS = $(CFLAGS) -O3 -Werror -Wall -I$(CURDIR)/compat/$(PLAT)

LDFLAGS_SHARED = $(LDFLAGS) -loctep_cp -lconfig -lrt

LDFLAGS_STATIC = $(LDFLAGS) -l:liboctep_cp.a -lconfig -lrt

STATIC_BIN = $(APP_NAME)
SHARED_BIN = $(APP_NAME)-shared
INSTALL_DIR = $(INSTALL_PATH)/bin

all: static shared install
.PHONY: shared static install clean

shared:
	$(info ====Building $(SHARED_BIN)====)
	$(CC) $(APP_CFLAGS) $(SRCS) -o $(SHARED_BIN) $(LDFLAGS_SHARED)

static:
	$(info ====Building $(STATIC_BIN)====)
	$(CC) $(APP_CFLAGS) $(SRCS) -o $(STATIC_BIN) $(LDFLAGS_STATIC)

install: static shared
	@mkdir -p $(INSTALL_DIR) || true
	mv $(STATIC_BIN) $(INSTALL_DIR)
	mv $(SHARED_BIN) $(INSTALL_DIR)

clean:
	$(info ====Cleaning apps====)
	@rm -f $(STATIC_BIN) $(SHARED_BIN) || true
	@rm -rf $(INSTALL_DIR) || true

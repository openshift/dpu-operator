#SPDX-License-Identifier: BSD-3-Clause
#Copyright (c) 2022 Marvell.

PLAT ?= aarch64

ifeq ($(INSTALL_PATH),)
INSTALL_PATH=$(CURDIR)/bin
endif

CC ?= $(CROSS_COMPILE)gcc
AR ?= $(CROSS_COMPILE)ar
RANLIB ?= $(CROSS_COMPILE)ranlib
LD ?= $(CROSS_COMPILE)ld

LIB = liboctep_cp

LIB_CFLAGS = $(CFLAGS) -O3 -Werror -Wall -Wundef -fPIC -g \
		-I$(CURDIR) \
		-I$(CURDIR)/include \
		-I$(CURDIR)/soc \
		-I$(CURDIR)/compat/$(PLAT) \
		-I$(CURDIR)/plugin \

LIB_LDFLAGS = $(LDFLAGS) -shared -fvisibility=hidden

SRCS = main.c
SRCS += soc/soc.c soc/cnxk.c
SRCS += soc/octep_ctrl_mbox.c
ifneq (,$(findstring USE_PEM_AND_DPI_PF=1,$(CFLAGS)))
SRCS += soc/vfio.c
endif
SRCS += plugin/server/octep_plugin_server.c
SRCS += plugin/client/octep_plugin_client.c

OBJS = main.o
ifneq (,$(findstring USE_PEM_AND_DPI_PF=1,$(CFLAGS)))
OBJS += vfio.o
endif
OBJS += soc.o cnxk.o octep_ctrl_mbox.o octep_plugin_server.o octep_plugin_client.o

STATIC_BIN = $(LIB).a
SHARED_BIN = $(LIB).so
INSTALL_INC_DIR = $(INSTALL_PATH)/include
INSTALL_LIB_DIR = $(INSTALL_PATH)/lib

all: shared static install
.PHONY: shared static install clean

static:
	$(info ====Building $(STATIC_BIN)====)
	$(CC) $(LIB_CFLAGS) -c $(SRCS)
	$(AR) rc $(STATIC_BIN) $(OBJS)
	$(RANLIB) $(STATIC_BIN)

shared:
	$(info ====Building $(SHARED_BIN)====)
	$(CC) $(LIB_CFLAGS) $(SRCS) -Wl,-soname,$(SHARED_BIN).1 -o $(SHARED_BIN).1.0.1 $(LIB_LDFLAGS)
	ln -sf $(SHARED_BIN).1.0.1 $(SHARED_BIN)

install: static shared
	mkdir -p $(INSTALL_INC_DIR) || true
	mkdir -p $(INSTALL_LIB_DIR) || true
	cp -f include/*.h $(INSTALL_INC_DIR)
	cp -df $(SHARED_BIN)* $(STATIC_BIN) $(INSTALL_LIB_DIR)

clean:
	$(info ====Cleaning lib====)
	@rm -f $(OBJS) $(SHARED_BIN)* $(STATIC_BIN) || true
	@rm -rf $(INSTALL_INC_DIR) $(INSTALL_LIB_DIR) || true

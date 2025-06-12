// SPDX-License-Identifier: BSD-3-Clause
/* Copyright (c) 2022 Marvell.
 */

#include <stdio.h>

#include "octep_cp_lib.h"
#include "cp_log.h"
#include "cp_lib.h"
#include "cnxk_nic.h"

int cnxk_nic_init(struct octep_cp_lib_cfg *p_cfg)
{
	CP_LIB_LOG(INFO, NIC, "init\n");

	return 0;
}

int cnxk_nic_poll(int max_events)
{
	CP_LIB_LOG(INFO, NIC, "poll\n");

	return 0;
}

int cnxk_nic_process_sigusr1()
{
	CP_LIB_LOG(INFO, NIC, "process_sigusr1\n");

	return 0;
}

int cnxk_nic_uninit()
{
	CP_LIB_LOG(INFO, NIC, "uninit\n");

	return 0;
}

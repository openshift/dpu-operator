// SPDX-License-Identifier: BSD-3-Clause
/* Copyright (c) 2022 Marvell.
 */
#include <errno.h>
#include <unistd.h>
#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include <stdlib.h>
#include <getopt.h>
#include <sys/stat.h>

#include "octep_cp_lib.h"
#include "cp_log.h"
#include "cp_lib.h"

/* operating state */
volatile enum cp_lib_state state = CP_LIB_STATE_INVALID;
/* user configuration */
struct octep_cp_lib_cfg user_cfg = {0};
/* soc operations */
static struct cp_lib_soc_ops *sops = NULL;
struct octep_cp_lib_cfg *lib_cfg;

static const char short_opts[] = {};
static const struct option long_opts[] = {
#if USE_PEM_AND_DPI_PF
	{"dpi_dev", 1, 0, 'd'},
	{"pem_dev", 1, 0, 'p'},
	{"sdp_rvu_pf", 1, 0, 's'},
#endif
	{NULL, 0, 0, 0}
};

/* Parse the command line arguments */
__attribute__((visibility("default")))
int octep_cp_lib_parse_args(int argc, char **argv, struct octep_cp_lib_cfg *cfg)
{
	int option_index, opt;
	int ret = 0;

	while ((opt = getopt_long(argc, argv, short_opts,
				  long_opts, &option_index)) != EOF) {
		switch (opt) {
#if USE_PEM_AND_DPI_PF
		case 'd': /* DPI device */
			if (cnxk_vfio_parse_dpi_dev(optarg))
				ret = -1;
			break;
		case 'p': /* PEM device */
			if (cnxk_vfio_parse_pem_dev(optarg))
				ret = -1;
			break;
		case 's': /* SDP RVU PF device */
			if (cnxk_vfio_parse_sdp_rvu_pf_dev(optarg))
				ret = -1;
			break;
#endif
		default:
			CP_LIB_LOG(ERR, CNXK, "Invalid option.\n");
			ret = -1;
			break;
		}
	}
	return ret;
}

__attribute__((visibility("default")))
int octep_cp_lib_init(struct octep_cp_lib_cfg *cfg)
{
	int err;

	CP_LIB_LOG(INFO, LIB, "init\n");
	lib_cfg = cfg;
	if (state >= CP_LIB_STATE_INIT)
		return 0;

	err = soc_get_ops(&sops);
	if (err || !sops)
		return -ENAVAIL;

	memset(&user_cfg, 0, sizeof(struct octep_cp_lib_cfg));
	state = CP_LIB_STATE_INIT;
	err = sops->init(cfg);
	if (err) {
		state = CP_LIB_STATE_INVALID;
		return err;
	}
	state = CP_LIB_STATE_READY;
	user_cfg = *cfg;

	return 0;
}

__attribute__((visibility("default")))
int octep_cp_lib_init_pem(struct octep_cp_lib_cfg *cfg, int dom_idx)
{
	int err;

	CP_LIB_LOG(INFO, LIB, "init PEM %d\n", dom_idx);
	if (state < CP_LIB_STATE_INIT)
		return -ENAVAIL;

	if (!sops)
		return -ENAVAIL;

	err = sops->init_pem(cfg, dom_idx);
	if (err)
		return err;

	user_cfg.doms[dom_idx] = cfg->doms[dom_idx];

	return 0;
}

__attribute__((visibility("default")))
int octep_cp_lib_get_info(struct octep_cp_lib_info *info)
{
	int err;

	//CP_LIB_LOG(INFO, LIB, "get info\n");
	if (state < CP_LIB_STATE_INIT)
		return -EAGAIN;

	if (!info)
		return -EINVAL;

	err = sops->get_info(info);
	if (err < 0)
		return err;

	err = soc_get_model(&info->soc_model);
	if (err < 0)
		return err;

	return 0;
}

__attribute__((visibility("default")))
int octep_cp_lib_send_msg_resp(union octep_cp_msg_info *ctx,
			       struct octep_cp_msg *msgs,
			       int num)
{
	//CP_LIB_LOG(INFO, LIB, "send message response\n");

	if (state != CP_LIB_STATE_READY)
		return -EAGAIN;

	if (!ctx || !msgs || num <= 0)
		return -EINVAL;

	return sops->send_msg_resp(ctx, msgs, num);
}

__attribute__((visibility("default")))
int octep_cp_lib_send_notification(union octep_cp_msg_info *ctx,
				   struct octep_cp_msg* msg)
{
	CP_LIB_LOG(INFO, LIB, "send notification\n");

	if (state != CP_LIB_STATE_READY)
		return -EAGAIN;

	if (!msg)
		return -EINVAL;

	return sops->send_notification(ctx, msg);
}

__attribute__((visibility("default")))
int octep_cp_lib_recv_msg(union octep_cp_msg_info *ctx,
			  struct octep_cp_msg *msgs,
			  int num)
{
	//CP_LIB_LOG(INFO, LIB, "receive message\n");

	if (state != CP_LIB_STATE_READY)
		return -EAGAIN;

	if (!ctx || !msgs || num <= 0)
		return -EINVAL;

	return sops->recv_msg(ctx, msgs, num);
}

__attribute__((visibility("default")))
int octep_cp_lib_send_event(struct octep_cp_event_info *info)
{
	//CP_LIB_LOG(INFO, LIB, "send event\n");

	if (state != CP_LIB_STATE_READY)
		return -EAGAIN;

	if (!info)
		return -EINVAL;

	return sops->send_event(info);
}

__attribute__((visibility("default")))
int octep_cp_lib_recv_event(struct octep_cp_event_info *info, int num)
{
	//CP_LIB_LOG(INFO, LIB, "receive event\n");

	if (state != CP_LIB_STATE_READY)
		return -EAGAIN;

	if (!info || num <= 0)
		return -EINVAL;

	return sops->recv_event(info, num);
}

__attribute__((visibility("default")))
int octep_cp_lib_uninit()
{
	CP_LIB_LOG(INFO, LIB, "uninit\n");

	if (state == CP_LIB_STATE_UNINIT || state == CP_LIB_STATE_INVALID)
		return 0;

	state = CP_LIB_STATE_UNINIT;
	sops->uninit();
	memset(&user_cfg, 0, sizeof(struct octep_cp_lib_cfg));
	sops = NULL;
	state = CP_LIB_STATE_INVALID;

	return 0;
}

__attribute__((visibility("default")))
int octep_cp_lib_uninit_pem(int dom_idx)
{
	CP_LIB_LOG(INFO, LIB, "uninit PEM %d\n", dom_idx);

	if (state == CP_LIB_STATE_UNINIT || state == CP_LIB_STATE_INVALID)
		return 0;

	sops->uninit_pem(dom_idx);
	memset(&user_cfg.doms[dom_idx], 0, sizeof(struct octep_cp_dom_cfg));

	return 0;
}

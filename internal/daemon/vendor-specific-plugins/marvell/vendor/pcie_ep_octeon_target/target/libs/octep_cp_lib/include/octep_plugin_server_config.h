/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __OCTEP_PLUGIN_SERVER_CONFIG_H__
#define __OCTEP_PLUGIN_SERVER_CONFIG_H__

#include <stdint.h>
#include <stdbool.h>

#include <octep_hw.h>

#ifndef ETH_ALEN
#define ETH_ALEN	6
#endif

#define PLUGIN_APP_CFG_PEM_MAX			8
#define PLUGIN_APP_CFG_PF_PER_PEM_MAX		128
#define PLUGIN_APP_CFG_VF_PER_PF_MAX		64

/* function config */
struct plugin_fn_cfg {
	/* Plugin control override flag */
	bool plugin_controlled;
	/* Plugin client id of master client app
	 * Valid only if plugin_controlled boolean
	 * is set
	 */
	int client_id;
};

/* Virtual function configuration */
struct plugin_vf_cfg {
	/* vf is valid */
	bool valid;
	/* config */
	struct plugin_fn_cfg fn;
};

/* Physical function configuration */
struct plugin_pf_cfg {
	/* pf is valid */
	bool valid;
	/* config */
	struct plugin_fn_cfg fn;
	/* number of vf's */
	int nvf;
	/* configuration for vf's */
	struct plugin_vf_cfg vfs[PLUGIN_APP_CFG_VF_PER_PF_MAX];
};

/* PEM configuration */
struct plugin_pem_cfg {
	/* pem is valid */
	bool valid;
	/* number of pf's */
	int npf;
	/* configuration for pf's */
	struct plugin_pf_cfg pfs[PLUGIN_APP_CFG_PF_PER_PEM_MAX];
};

/* app configuration */
struct plugin_app_cfg {
	/* number of pem's */
	int npem;
	/* configuration for pem's */
	struct plugin_pem_cfg pems[PLUGIN_APP_CFG_PEM_MAX];
};

/* Get pf/vf config based on information in message header.
 *
 * @param cfg: non-null pointer to struct plugin_app_cfg *.
 * @param msg: non-null pointer to message info.
 *
 * return value: pointer to valid struct plugin_fn_cfg* on success, NULL on failure.
 */
static inline struct plugin_fn_cfg *plugin_app_config_get_fn(struct plugin_app_cfg *p_cfg,
					       union octep_cp_msg_info *msg)
{
	struct plugin_pf_cfg *pf;

	if (msg->s.pem_idx >= PLUGIN_APP_CFG_PEM_MAX)
		return NULL;

	if (msg->s.pf_idx >= PLUGIN_APP_CFG_PF_PER_PEM_MAX)
		return NULL;

	if (msg->s.is_vf && msg->s.vf_idx >= PLUGIN_APP_CFG_VF_PER_PF_MAX)
		return NULL;

	pf = &(p_cfg->pems[msg->s.pem_idx].pfs[msg->s.pf_idx]);

	return (msg->s.is_vf) ? &(pf->vfs[msg->s.vf_idx].fn) : &pf->fn;
}

#endif /* __APP_CONFIG_H__ */

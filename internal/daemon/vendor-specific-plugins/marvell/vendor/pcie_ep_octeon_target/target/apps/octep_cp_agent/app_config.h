/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __APP_CONFIG_H__
#define __APP_CONFIG_H__

#include <stdint.h>
#include <stdbool.h>

#include <octep_hw.h>

#ifndef ETH_ALEN
#define ETH_ALEN	6
#endif

#define APP_CFG_PEM_MAX			8
#define APP_CFG_PF_PER_PEM_MAX		128
#define APP_CFG_VF_PER_PF_MAX		64

#define MIN_HB_INTERVAL_MSECS		1000
#define MAX_HB_INTERVAL_MSECS		15000
#define DEFAULT_HB_INTERVAL_MSECS	MIN_HB_INTERVAL_MSECS

#define DEFAULT_HB_MISS_COUNT		20

/* Network interface stats */
struct if_stats {
	struct octep_iface_rx_stats rx_stats;
	struct octep_iface_tx_stats tx_stats;
};

/* Network interface data */
struct if_cfg {
	uint16_t host_if_id;
	/* current MTU of the interface */
	uint16_t mtu;
	/* interface mac address */
	uint8_t mac_addr[ETH_ALEN];
	/* enum octep_ctrl_net_state */
	uint16_t link_state;
	/* enum octep_ctrl_net_state */
	uint16_t rx_state;
	/* OCTEP_LINK_MODE_XXX */
	uint8_t autoneg;
	/* OCTEP_LINK_MODE_XXX */
	uint8_t pause_mode;
	/* SPEED_XXX */
	uint32_t speed;
	/* OCTEP_LINK_MODE_XXX */
	uint64_t supported_modes;
	/* OCTEP_LINK_MODE_XXX */
	uint64_t advertised_modes;
};

/* function config */
struct fn_cfg {
	/* network interface data */
	struct if_cfg iface;
	/* network interface stats */
	struct if_stats ifstats;
	/* interface info */
	struct octep_fw_info info;
};

/* Virtual function configuration */
struct vf_cfg {
	/* vf is valid */
	bool valid;
	/* config */
	struct fn_cfg fn;
};

/* Physical function configuration */
struct pf_cfg {
	/* pf is valid */
	bool valid;
	/* config */
	struct fn_cfg fn;
	/* number of vf's */
	int nvf;
	/* configuration for vf's */
	struct vf_cfg vfs[APP_CFG_VF_PER_PF_MAX];
};

/* PEM configuration */
struct pem_cfg {
	/* pem is valid */
	bool valid;
	/* number of pf's */
	int npf;
	/* configuration for pf's */
	struct pf_cfg pfs[APP_CFG_PF_PER_PEM_MAX];
};

/* app configuration */
struct app_cfg {
	/* number of pem's */
	int npem;
	/* configuration for pem's */
	struct pem_cfg pems[APP_CFG_PEM_MAX];
};

extern struct app_cfg cfg;

/* Parse file and populate configuration.
 *
 * @param cfg_file_path: Path to configuration file.
 *
 * return value: 0 on success, -errno on failure.
 */
int app_config_init(const char *cfg_file_path);

/* Get pf/vf config based on information in message header.
 *
 * @param cfg: non-null pointer to struct app_cfg *.
 * @param msg: non-null pointer to message info.
 *
 * return value: pointer to valid struct fn_cfg* on success, NULL on failure.
 */
static inline struct fn_cfg *app_config_get_fn(struct app_cfg *p_cfg,
					       union octep_cp_msg_info *msg)
{
	struct pf_cfg *pf;

	if (msg->s.pem_idx >= APP_CFG_PEM_MAX)
		return NULL;

	if (msg->s.pf_idx >= APP_CFG_PF_PER_PEM_MAX)
		return NULL;

	if (msg->s.is_vf && msg->s.vf_idx >= APP_CFG_VF_PER_PF_MAX)
		return NULL;

	pf = &(p_cfg->pems[msg->s.pem_idx].pfs[msg->s.pf_idx]);

	return (msg->s.is_vf) ? &(pf->vfs[msg->s.vf_idx].fn) : &pf->fn;
}

/* Update/adjust app configuration.
 *
 * This can be called after octep_cp_lib is initialized.
 * App can then use library info to adjust its configuration based on
 * any available runtime information.
 *
 * return value: 0 on success, -errno on failure.
 */
int app_config_update();

/* Update/adjust app configuration for a pem.
 *
 * This can be called after initializing a pem after a reset operation
 * App can then use library info to adjust its configuration based on
 * any available runtime information.
 *
 * return value: 0 on success, -errno on failure.
 */
int app_config_update_pem(int dom_idx);

/* Print app configuration.
 *
 *
 * return value: 0 on success, -errno on failure.
 */
int app_config_print();

/* Print app configuration for a pem
 *
 *
 * return value: 0 on success, -errno on failure.
 */
int app_config_print_pem(int dom_idx);

/* Free allocated configuration artifacts.
 *
 *
 * return value: 0 on success, -errno on failure.
 */
int app_config_uninit();

#endif /* __APP_CONFIG_H__ */

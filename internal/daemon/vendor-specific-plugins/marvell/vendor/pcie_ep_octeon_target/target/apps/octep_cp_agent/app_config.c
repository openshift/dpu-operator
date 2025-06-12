// SPDX-License-Identifier: BSD-3-Clause
/* Copyright (c) 2022 Marvell.
 */
#include <stdlib.h>
#include <errno.h>
#include <stdint.h>
#include <libconfig.h>
#include <string.h>

#include "octep_cp_lib.h"
#include "app_config.h"

struct app_cfg cfg;

/**
 * Object heirarchy
 * *(0 or more), +(1 or more)
 *
 * soc = { pem* };
 * pem = { idx, pf* };
 * pf = { idx, if, info, vf* };
 * vf = { idx, if, info };
 * if = { mac_addr, link_state, rx_state, autoneg, pause_mode, speed,
 *        supported_modes, advertisedd_modes
 * };
 * info = { pkind, hb_interval, hb_miss_count };
 */

#define CFG_TOKEN_SOC			"soc"
#define CFG_TOKEN_PEMS			"pems"
#define CFG_TOKEN_PFS			"pfs"
#define CFG_TOKEN_VFS			"vfs"
#define CFG_TOKEN_IDX			"idx"
#define CFG_TOKEN_IF_MAC_ADDR		"mac_addr"
#define CFG_TOKEN_IF_LSTATE		"link_state"
#define CFG_TOKEN_IF_RSTATE		"rx_state"
#define CFG_TOKEN_IF_AUTONEG		"autoneg"
#define CFG_TOKEN_IF_PMODE		"pause_mode"
#define CFG_TOKEN_IF_SPEED		"speed"
#define CFG_TOKEN_IF_SMODES		"supported_modes"
#define CFG_TOKEN_IF_AMODES		"advertised_modes"
#define CFG_TOKEN_INFO_PKIND		"pkind"
#define CFG_TOKEN_INFO_HB_INTERVAL	"hb_interval"
#define CFG_TOKEN_INFO_HB_MISS_COUNT	"hb_miss_count"

static inline struct pem_cfg *get_pem(int idx)
{
	if (idx >= APP_CFG_PEM_MAX)
		return NULL;

	return &cfg.pems[idx];
}

static inline struct pf_cfg *get_pf(struct pem_cfg *pemcfg, int idx)
{
	if (idx >= APP_CFG_PF_PER_PEM_MAX)
		return NULL;

	return &pemcfg->pfs[idx];
}

static inline struct vf_cfg *get_vf(struct pf_cfg *pfcfg, int idx)
{
	if (idx >= APP_CFG_VF_PER_PF_MAX)
		return NULL;

	return &pfcfg->vfs[idx];
}

static int parse_if(config_setting_t *lcfg, struct if_cfg *iface)
{
	config_setting_t *mac;
	int ival, i, n;

	mac = config_setting_get_member(lcfg, CFG_TOKEN_IF_MAC_ADDR);
	if (mac) {
		n = config_setting_length(mac);
		if (n > ETH_ALEN)
			n = ETH_ALEN;
		for (i=0; i<n; i++)
			iface->mac_addr[i] = config_setting_get_int_elem(mac,
									 i);
	}
	if (config_setting_lookup_int(lcfg, CFG_TOKEN_IF_LSTATE, &ival))
		iface->link_state = ival;
	if (config_setting_lookup_int(lcfg, CFG_TOKEN_IF_RSTATE, &ival))
		iface->rx_state = ival;
	if (config_setting_lookup_int(lcfg, CFG_TOKEN_IF_AUTONEG, &ival))
		iface->autoneg = ival;
	if (config_setting_lookup_int(lcfg, CFG_TOKEN_IF_PMODE, &ival))
		iface->pause_mode = ival;
	if (config_setting_lookup_int(lcfg, CFG_TOKEN_IF_SPEED, &ival))
		iface->speed = ival;
	if (config_setting_lookup_int(lcfg, CFG_TOKEN_IF_SMODES, &ival))
		iface->supported_modes = ival;
	if (config_setting_lookup_int(lcfg, CFG_TOKEN_IF_AMODES, &ival))
		iface->advertised_modes = ival;

	return 0;
}

static int parse_info(config_setting_t *lcfg, struct octep_fw_info *info)
{
	int ival, ret;

	ret = config_setting_lookup_int(lcfg, CFG_TOKEN_INFO_PKIND, &ival);
	info->pkind = (ret == CONFIG_TRUE) ? ival : 0;

	ret = config_setting_lookup_int(lcfg, CFG_TOKEN_INFO_HB_INTERVAL, &ival);
	info->hb_interval = (ret == CONFIG_TRUE) ?
			     ival : DEFAULT_HB_INTERVAL_MSECS;

	ret = config_setting_lookup_int(lcfg, CFG_TOKEN_INFO_HB_MISS_COUNT, &ival);
	info->hb_miss_count = (ret == CONFIG_TRUE) ?
			       ival : DEFAULT_HB_MISS_COUNT;

	return 0;
}

static int parse_fn(config_setting_t *lcfg, struct fn_cfg *fn)
{
	int err;

	err = parse_if(lcfg, &fn->iface);
	if (err)
		return err;

	err = parse_info(lcfg, &fn->info);
	if (err)
		return err;

	return 0;
}

static int parse_pf(config_setting_t *pf, struct pf_cfg *pfcfg, int pf_idx)
{
	config_setting_t *vfs, *vf;
	int nvfs, i, idx, err;
	struct vf_cfg *vfcfg;

	err = parse_fn(pf, &pfcfg->fn);
	if (err)
		return err;

	vfs = config_setting_get_member(pf, CFG_TOKEN_VFS);
	if (!vfs)
		return 0;

	nvfs = config_setting_length(vfs);
	for (i = 0; i < nvfs; i++) {
		vf = config_setting_get_elem(vfs, i);
		if (!vf)
			continue;
		if (config_setting_lookup_int(vf, CFG_TOKEN_IDX, &idx) ==
		    CONFIG_FALSE)
			continue;
		vfcfg = get_vf(pfcfg, idx);
		if (!vfcfg) {
			printf("APP: Skipping out of bounds pf[%d]vf[%d]\n",
			       pf_idx, idx);
			continue;
		}
		err = parse_fn(vf, &vfcfg->fn);
		if (err)
			return err;

		vfcfg->valid = true;
	}
	pfcfg->nvf = nvfs;

	return 0;
}

static int parse_pem(config_setting_t *pem, struct pem_cfg *pemcfg, int pem_idx)
{
	config_setting_t *pfs, *pf;
	int npfs, i, idx, err;
	struct pf_cfg *pfcfg;

	pfs = config_setting_get_member(pem, CFG_TOKEN_PFS);
	if (!pfs)
		return 0;

	npfs = config_setting_length(pfs);
	for (i = 0; i < npfs; i++) {
		pf = config_setting_get_elem(pfs, i);
		if (!pf)
			continue;
		if (config_setting_lookup_int(pf, CFG_TOKEN_IDX, &idx) ==
		    CONFIG_FALSE)
			continue;
		pfcfg = get_pf(pemcfg, idx);
		if (!pfcfg) {
			printf("APP: Skipping out of bounds pem[%d]pf[%d]\n",
		   		   pem_idx, idx);
			continue;
		}
		err = parse_pf(pf, pfcfg, i);
		if (err)
			return err;

		pfcfg->valid = true;
	}
	pemcfg->npf = npfs;

	return 0;
}

static int parse_pems(config_setting_t *pems)
{
	config_setting_t *pem;
	int npems, i, idx, err;
	struct pem_cfg *pemcfg;

	npems = config_setting_length(pems);
	for (i = 0; i < npems; i++) {
		pem = config_setting_get_elem(pems, i);
		if (!pem)
			continue;
		if (config_setting_lookup_int(pem, CFG_TOKEN_IDX, &idx) ==
		    CONFIG_FALSE)
			continue;
		pemcfg = get_pem(idx);
		if (!pemcfg) {
			printf("APP: Skipping out of bounds pem[%d]\n", idx);
			continue;
		}
		err = parse_pem(pem, pemcfg, idx);
		if (err)
			return err;

		pemcfg->valid = true;
	}
	cfg.npem = npems;

	return 0;
}

int app_config_init(const char *cfg_file_path)
{
	config_setting_t *lcfg, *pems;
	config_t fcfg;
	int err;

	memset (&cfg, 0, sizeof(struct app_cfg));
	printf("APP: config init : %s\n", cfg_file_path);
	config_init(&fcfg);
	if (!config_read_file(&fcfg, cfg_file_path)) {
		printf("APP: %s:%d - %s\n",
		       config_error_file(&fcfg),
		       config_error_line(&fcfg),
		       config_error_text(&fcfg));
		config_destroy(&fcfg);
		return -EINVAL;
	}

	lcfg = config_lookup(&fcfg, CFG_TOKEN_SOC);
	if (!lcfg) {
		config_destroy(&fcfg);
		return -EINVAL;
	}

	pems = config_setting_get_member(lcfg, CFG_TOKEN_PEMS);
	if (pems) {
		err = parse_pems(pems);
		if (err) {
			config_destroy(&fcfg);
			return err;
		}
	}

	config_destroy(&fcfg);

	return 0;
}

static int update_fn(struct fn_cfg *fn, struct octep_cp_lib_info *info)
{
	/* Initialize mtu according to max rx pktlen supported by soc */
	/* Errata IPBUNIXTX-35039 */
	fn->iface.mtu = (info->soc_model.flag &
			 (OCTEP_CP_SOC_MODEL_CN96xx_Ax |
			  OCTEP_CP_SOC_MODEL_CNF95xxN_A0 |
			  OCTEP_CP_SOC_MODEL_CNF95xxO_A0)) ?
					(16 * 1024) : ((64 * 1024) - 1);

	return 0;
}

int app_config_update()
{
	struct pem_cfg *pem;
	int i;

	for (i = 0; i < APP_CFG_PEM_MAX; i++) {
		pem = &cfg.pems[i];
		if (!pem->valid)
			continue;

		app_config_update_pem(i);
	}

	return 0;
}

int app_config_update_pem(int dom_idx)
{
	struct octep_cp_lib_info info;
	struct pem_cfg *pem;
	struct pf_cfg *pf;
	struct vf_cfg *vf;
	int j, k;

	if (dom_idx >= APP_CFG_PEM_MAX) {
		printf("APP: Invalid domain index: %d\n",
		       dom_idx);
		return -EINVAL;
	}

	octep_cp_lib_get_info(&info);
	pem = &cfg.pems[dom_idx];
	if (!pem->valid)
		return -EINVAL;

	for (j = 0; j < pem->npf; j++) {
		pf = &pem->pfs[j];
		if (!pf->valid)
			continue;

		update_fn(&pf->fn, &info);
		for (k = 0; k < APP_CFG_VF_PER_PF_MAX; k++) {
			vf = &pf->vfs[k];
			if (!vf->valid)
				continue;

			update_fn(&vf->fn, &info);
		}
	}

	return 0;
}

static void print_if(struct if_cfg *iface)
{
	printf("APP: mac_addr: %02x:%02x:%02x:%02x:%02x:%02x\n",
	       iface->mac_addr[0], iface->mac_addr[1],
	       iface->mac_addr[2], iface->mac_addr[3],
	       iface->mac_addr[4], iface->mac_addr[5]);
	printf("APP: mtu: %d, link: %d, rx: %d, autoneg: 0x%x\n",
	       iface->mtu, iface->link_state, iface->rx_state,
	       iface->autoneg);
	printf("APP: pause_mode: 0x%x, speed: %d\n",
	       iface->pause_mode, iface->speed);
	printf("APP: supported_modes: 0x%lx, advertised_modes: 0x%lx\n",
	       iface->supported_modes, iface->advertised_modes);
}

static void print_info(struct octep_fw_info *info)
{
	printf("APP: pkind: %u, hbi: %u, hbmc: %u\n",
	       info->pkind, info->hb_interval, info->hb_miss_count);
}

int app_config_print()
{
	struct pem_cfg *pem;
	int i;

	for (i = 0; i < APP_CFG_PEM_MAX; i++) {
		pem = &cfg.pems[i];
		if (!pem->valid)
			continue;

		app_config_print_pem(i);
	}

	return 0;
}

int app_config_print_pem(int dom_idx)
{
	struct pem_cfg *pem;
	struct pf_cfg *pf;
	struct vf_cfg *vf;
	int j, k;

	pem = &cfg.pems[dom_idx];
	if (!pem->valid)
		return -EINVAL;

	for (j = 0; j < pem->npf; j++) {
		pf = &pem->pfs[j];
		if (!pf->valid)
			continue;

		printf("APP: [%d]:[%d]\n", dom_idx, j);
		print_if(&pf->fn.iface);
		print_info(&pf->fn.info);
		for (k = 0; k < APP_CFG_VF_PER_PF_MAX; k++) {
			vf = &pf->vfs[k];
			if (!vf->valid)
				continue;

			printf("APP: [%d]:[%d]:[%d]\n", dom_idx, j, k);
			print_if(&vf->fn.iface);
			print_info(&vf->fn.info);
		}
	}

	return 0;
}

int app_config_uninit()
{
	printf("APP: config uninit\n");
	memset (&cfg, 0, sizeof(struct app_cfg));

	return 0;
}

/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __CP_LIB_H__
#define __CP_LIB_H__

#ifndef ETH_ALEN
#define ETH_ALEN	6
#endif

#define IS_SOC_CN10K	(soc == CP_LIB_SOC_CNXK)

/* Supported soc's */
enum cp_lib_soc {
        CP_LIB_SOC_OTX2,
        CP_LIB_SOC_CNXK,
        CP_LIB_SOC_MAX
};

/* library state */
enum cp_lib_state {
	CP_LIB_STATE_INVALID,
	CP_LIB_STATE_INIT,
	CP_LIB_STATE_READY,
	CP_LIB_STATE_UNINIT,
};

/* Physical function configuration */
struct cp_lib_pf {
	/* pf index */
	int idx;
	struct cp_lib_pf *next;
};

/* PEM configuration */
struct cp_lib_pem {
	/* pem index */
	int idx;
	/* number of pf's */
	int npf;
	/* configuration for pf's */
	struct cp_lib_pf *pfs;
	struct cp_lib_pem *next;
};

/* library configuration */
struct cp_lib_cfg {
	/* soc implementation to be autodetected */
	enum cp_lib_soc soc;
	/* number of pem's */
	int npem;
	/* configuration for pem's */
	struct cp_lib_pem *pems;
};

/* soc operations */
struct cp_lib_soc_ops {
	/* initialize */
	int (*init)(struct octep_cp_lib_cfg *p_cfg);
	/* initialize a pem */
	int (*init_pem)(struct octep_cp_lib_cfg *p_cfg, int dom_idx);
	/* get info */
	int (*get_info)(struct octep_cp_lib_info *info);
	/* send message responses to host */
	int (*send_msg_resp)(union octep_cp_msg_info *ctx,
			     struct octep_cp_msg *msg, int num);
	/* send notification to host */
	int (*send_notification)(union octep_cp_msg_info *ctx,
				 struct octep_cp_msg* msg);
	/* receive messages from host*/
	int (*recv_msg)(union octep_cp_msg_info *ctx,
			struct octep_cp_msg *msg, int num);
	/* send event to host */
	int (*send_event)(struct octep_cp_event_info *info);
	/* receive soc events */
	int (*recv_event)(struct octep_cp_event_info *info, int num);
	/* uninitialize pem */
	int (*uninit_pem)(int dom_idx);
	/* uninitialize */
	int (*uninit)(void);
};

extern volatile enum cp_lib_state state;
extern struct octep_cp_lib_cfg user_cfg;
extern enum cp_lib_soc soc;

/* Get soc ops.
 *
 * @param mode: mode to be used.
 * @param ops: non-null pointer to struct soc_ops* to be filled by soc impl.
 *
 * return value: 0 on success, -errno on failure.
 */
int soc_get_ops(struct cp_lib_soc_ops **ops);

/* Get soc model.
 *
 * @param model: non-null pointer to struct octep_cp_soc_model.
 *
 * return value: 0 on success, -errno on failure.
 */
int soc_get_model(struct octep_cp_soc_model *sm);

/* Parse file and populate configuration.
 *
 * @param cfg_file_path: Path to configuration file.
 *
 * return value: 0 on success, -errno on failure.
 */
int lib_config_init(const char *cfg_file_path);

/* Free allocated configuration artifacts.
 *
 *
 * return value: 0 on success, -errno on failure.
 */
int lib_config_uninit();

#endif /* __CP_LIB_H__ */

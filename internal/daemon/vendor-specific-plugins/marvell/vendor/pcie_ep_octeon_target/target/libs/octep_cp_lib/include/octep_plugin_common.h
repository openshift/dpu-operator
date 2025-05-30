/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __OCTEP_PLUGIN_H__
#define __OCTEP_PLUGIN_H__

#ifdef __cplusplus
extern "C" {
#endif

#define OCTEP_PLUGIN_VERSION(a, b, c)		(((a & 0xff) << 16) + \
						 ((b & 0xff) << 8) + \
						 (c & 0xff))
#define OCTEP_PLUGIN_SERVER_PORT		49500
#define OCTEP_PLUGIN_MAX_CLIENTS		2

#define OCTEP_PLUGIN_MAX_PEM                        8
#define OCTEP_PLUGIN_MAX_PF_PER_PEM                 128
#define OCTEP_PLUGIN_MAX_VF_PER_PF		    64
#define OCTEP_PLUGIN_INVALID_VF_IDX		    OCTEP_PLUGIN_MAX_VF_PER_PF
#define OCTEP_PLUGIN_MSG_MAX_LEN		    2048

/* Plugin client to server messages */
enum {
	OCTEP_PLUGIN_C2S_MSG_INVALID,
	/* Initialize plugin */
	OCTEP_PLUGIN_C2S_MSG_INIT,
	/* Register a handler for pf/vf */
	OCTEP_PLUGIN_C2S_MSG_DEV_REGISTER,
	/* unregister a handler for pf/vf */
	OCTEP_PLUGIN_C2S_MSG_DEV_UNREGISTER,
	/* octep ctrl net notifications */
	OCTEP_PLUGIN_C2S_MSG_CTRL_NET_NOTIFY,
	OCTEP_PLUGIN_C2S_MSG_CTRL_NET_RESP,
	OCTEP_PLUGIN_C2S_MSG_MAX
};

/* Plugin server to client messages */
enum {
	OCTEP_PLUGIN_S2C_MSG_INVALID,
	OCTEP_PLUGIN_S2C_MSG_PLUGIN_RESP,
	/* forwarded octep ctrl net message */
	OCTEP_PLUGIN_S2C_MSG_CTRL_NET,
	OCTEP_PLUGIN_S2C_MSG_HOST_VERSION,
	OCTEP_PLUGIN_S2C_MSG_MAX
};

/* Plugin info */
struct octep_plugin_info {
	/* reserved */
	uint32_t reserved;
};

/* Plugin device Identifier */
struct octep_plugin_dev_id {
	/* pem index [0..OCTEP_PLUGIN_MAX_PEM - 1]*/
	uint16_t pem;
	/* pf index [0..OCTEP_PLUGIN_MAX_PF_PER_PEM - 1]*/
	uint16_t pf;
	/* vf index [0..OCTEP_PLUGIN_MAX_VF_PER_PF - 1] or
	 * OCTEP_PLUGIN_INVALID_VF_IDX when referring to pf only
	 */
	uint16_t vf;
};

struct octep_plugin_msg_hdr {
	/* OCTEP_PLUGIN_C2S_* or OCTEP_PLUGIN_S2C_* */
	uint32_t id;
	/* Controlled Device ID */
	struct octep_plugin_dev_id dev_id;
	/* message size */
	uint32_t sz;
};

struct octep_plugin_msg {
	/* header */
	struct octep_plugin_msg_hdr hdr;
	/* message data */
	uint8_t data[OCTEP_PLUGIN_MSG_MAX_LEN];
};

#ifdef __cplusplus
}
#endif

#endif /* __OCTEP_PLUGIN_H__ */

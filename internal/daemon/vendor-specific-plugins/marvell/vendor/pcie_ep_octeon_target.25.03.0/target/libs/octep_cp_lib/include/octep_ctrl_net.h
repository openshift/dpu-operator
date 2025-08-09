/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __OCTEP_CTRL_NET_H__
#define __OCTEP_CTRL_NET_H__

#include "octep_cp_version.h"
#include "octep_hw.h"

#ifndef ETH_ALEN
#define ETH_ALEN	6
#endif

/* Supported commands */
enum octep_ctrl_net_cmd {
	OCTEP_CTRL_NET_CMD_GET = 0,
	OCTEP_CTRL_NET_CMD_SET,
};

/* Supported states */
enum octep_ctrl_net_state {
	OCTEP_CTRL_NET_STATE_DOWN = 0,
	OCTEP_CTRL_NET_STATE_UP,
};

/* Supported replies */
enum octep_ctrl_net_reply {
	OCTEP_CTRL_NET_REPLY_OK = 0,
	OCTEP_CTRL_NET_REPLY_GENERIC_FAIL,
	OCTEP_CTRL_NET_REPLY_INVALID_PARAM,
	OCTEP_CTRL_NET_REPLY_UNSUPPORTED
};

/* Supported host to fw commands */
enum octep_ctrl_net_h2f_cmd {
	OCTEP_CTRL_NET_H2F_CMD_INVALID = 0,
	OCTEP_CTRL_NET_H2F_CMD_MTU,
	OCTEP_CTRL_NET_H2F_CMD_MAC,
	OCTEP_CTRL_NET_H2F_CMD_GET_IF_STATS,
	OCTEP_CTRL_NET_H2F_CMD_GET_XSTATS,
	OCTEP_CTRL_NET_H2F_CMD_GET_Q_STATS,
	OCTEP_CTRL_NET_H2F_CMD_LINK_STATUS,
	OCTEP_CTRL_NET_H2F_CMD_RX_STATE,
	OCTEP_CTRL_NET_H2F_CMD_LINK_INFO,
	OCTEP_CTRL_NET_H2F_CMD_GET_INFO,
	OCTEP_CTRL_NET_H2F_CMD_DEV_REMOVE,
	OCTEP_CTRL_NET_H2F_CMD_OFFLOADS,
	OCTEP_CTRL_NET_H2F_CMD_MAX
};

/* Control plane version in which OCTEP_CTRL_NET_H2F_CMD was added */
static const uint32_t octep_ctrl_net_h2f_cmd_versions[OCTEP_CTRL_NET_H2F_CMD_MAX] = {
	[OCTEP_CTRL_NET_H2F_CMD_INVALID] = OCTEP_CP_VERSION(1, 0, 0),
	[OCTEP_CTRL_NET_H2F_CMD_MTU] = OCTEP_CP_VERSION(1, 0, 0),
	[OCTEP_CTRL_NET_H2F_CMD_MAC] = OCTEP_CP_VERSION(1, 0, 0),
	[OCTEP_CTRL_NET_H2F_CMD_GET_IF_STATS] = OCTEP_CP_VERSION(1, 0, 0),
	[OCTEP_CTRL_NET_H2F_CMD_GET_XSTATS] = OCTEP_CP_VERSION(1, 0, 0),
	[OCTEP_CTRL_NET_H2F_CMD_GET_Q_STATS] = OCTEP_CP_VERSION(1, 0, 0),
	[OCTEP_CTRL_NET_H2F_CMD_LINK_STATUS] = OCTEP_CP_VERSION(1, 0, 0),
	[OCTEP_CTRL_NET_H2F_CMD_RX_STATE] = OCTEP_CP_VERSION(1, 0, 0),
	[OCTEP_CTRL_NET_H2F_CMD_LINK_INFO] = OCTEP_CP_VERSION(1, 0, 0),
	[OCTEP_CTRL_NET_H2F_CMD_GET_INFO] = OCTEP_CP_VERSION(1, 0, 0),
	[OCTEP_CTRL_NET_H2F_CMD_DEV_REMOVE] = OCTEP_CP_VERSION(1, 0, 0),
	[OCTEP_CTRL_NET_H2F_CMD_OFFLOADS] = OCTEP_CP_VERSION(1, 0, 1)
};

/* Supported fw to host commands */
enum octep_ctrl_net_f2h_cmd {
	OCTEP_CTRL_NET_F2H_CMD_INVALID = 0,
	OCTEP_CTRL_NET_F2H_CMD_LINK_STATUS,
	OCTEP_CTRL_NET_F2H_CMD_MAX
};

/* Control plane version in which OCTEP_CTRL_NET_F2H_CMD was added */
static const uint32_t octep_ctrl_net_f2h_cmd_versions[OCTEP_CTRL_NET_F2H_CMD_MAX] = {
	[OCTEP_CTRL_NET_F2H_CMD_INVALID] = OCTEP_CP_VERSION(1, 0, 0),
	[OCTEP_CTRL_NET_F2H_CMD_LINK_STATUS] = OCTEP_CP_VERSION(1, 0, 0)
};

union octep_ctrl_net_req_hdr {
	uint64_t words[1];
	struct {
		/* sender id */
		uint16_t sender;
		/* receiver id */
		uint16_t receiver;
		/* octep_ctrl_net_h2t_cmd */
		uint16_t cmd;
		/* reserved */
		uint16_t rsvd0;
	} s;
};

/* get/set mtu request */
struct octep_ctrl_net_h2f_req_cmd_mtu {
	/* enum octep_ctrl_net_cmd */
	uint16_t cmd;
	/* 0-65535 */
	uint16_t val;
};

/* get/set mac request */
struct octep_ctrl_net_h2f_req_cmd_mac {
	/* enum octep_ctrl_net_cmd */
	uint16_t cmd;
	/* xx:xx:xx:xx:xx:xx */
	uint8_t addr[ETH_ALEN];
};

/* get/set link state, rx state */
struct octep_ctrl_net_h2f_req_cmd_state {
	/* enum octep_ctrl_net_cmd */
	uint16_t cmd;
	/* enum octep_ctrl_net_state */
	uint16_t state;
};

/* link info */
struct octep_ctrl_net_link_info {
	/* Bitmap of Supported link speeds/modes */
	uint64_t supported_modes;
	/* Bitmap of Advertised link speeds/modes */
	uint64_t advertised_modes;
	/* Autonegotation state; bit 0=disabled; bit 1=enabled */
	uint8_t autoneg;
	/* Pause frames setting. bit 0=disabled; bit 1=enabled */
	uint8_t pause;
	/* Negotiated link speed in Mbps */
	uint32_t speed;
};

/* get/set link info */
struct octep_ctrl_net_h2f_req_cmd_link_info {
	/* enum octep_ctrl_net_cmd */
	uint16_t cmd;
	/* struct octep_ctrl_net_link_info */
	struct octep_ctrl_net_link_info info;
};

/* offloads */
struct octep_ctrl_net_offloads {
	/* supported rx offloads OCTEP_RX_OFFLOAD_* */
	uint16_t rx_offloads;
	/* supported tx offloads OCTEP_TX_OFFLOAD_* */
	uint16_t tx_offloads;
	/* reserved */
	uint32_t reserved_offloads;
	/* extra offloads */
	uint64_t ext_offloads;
};

/* get/set offloads */
struct octep_ctrl_net_h2f_req_cmd_offloads {
	/* enum octep_ctrl_net_cmd */
	uint16_t cmd;
	/* struct octep_ctrl_net_offloads */
	struct octep_ctrl_net_offloads offloads;
};

/* Host to fw request data */
struct octep_ctrl_net_h2f_req {
	union octep_ctrl_net_req_hdr hdr;
	union {
		struct octep_ctrl_net_h2f_req_cmd_mtu mtu;
		struct octep_ctrl_net_h2f_req_cmd_mac mac;
		struct octep_ctrl_net_h2f_req_cmd_state link;
		struct octep_ctrl_net_h2f_req_cmd_state rx;
		struct octep_ctrl_net_h2f_req_cmd_link_info link_info;
		struct octep_ctrl_net_h2f_req_cmd_offloads offloads;
	};
} __attribute__((__packed__));

union octep_ctrl_net_resp_hdr {
	uint64_t words[1];
	struct {
		/* sender id */
		uint16_t sender;
		/* receiver id */
		uint16_t receiver;
		/* octep_ctrl_net_h2t_cmd */
		uint16_t cmd;
		/* octep_ctrl_net_reply */
		uint16_t reply;
	} s;
};

/* get mtu response */
struct octep_ctrl_net_h2f_resp_cmd_mtu {
	/* 0-65535 */
	uint16_t val;
};

/* get mac response */
struct octep_ctrl_net_h2f_resp_cmd_mac {
	/* xx:xx:xx:xx:xx:xx */
	uint8_t addr[ETH_ALEN];
};

/* get if_stats, xstats, q_stats request */
struct octep_ctrl_net_h2f_resp_cmd_get_stats {
	struct octep_iface_rx_stats rx_stats;
	struct octep_iface_tx_stats tx_stats;
};

/* get link state, rx state response */
struct octep_ctrl_net_h2f_resp_cmd_state {
	/* enum octep_ctrl_net_state */
	uint16_t state;
};

/* get info request */
struct octep_ctrl_net_h2f_resp_cmd_get_info {
	struct octep_fw_info fw_info;
};

/* Host to fw response data */
struct octep_ctrl_net_h2f_resp {
	union octep_ctrl_net_resp_hdr hdr;
	union {
		struct octep_ctrl_net_h2f_resp_cmd_mtu mtu;
		struct octep_ctrl_net_h2f_resp_cmd_mac mac;
		struct octep_ctrl_net_h2f_resp_cmd_get_stats if_stats;
		struct octep_ctrl_net_h2f_resp_cmd_state link;
		struct octep_ctrl_net_h2f_resp_cmd_state rx;
		struct octep_ctrl_net_link_info link_info;
		struct octep_ctrl_net_h2f_resp_cmd_get_info info;
		struct octep_ctrl_net_offloads offloads;
	};
}__attribute__((__packed__));

/* link state notofication */
struct octep_ctrl_net_f2h_req_cmd_state {
	/* enum octep_ctrl_net_state */
	uint16_t state;
};

/* Fw to host request data */
struct octep_ctrl_net_f2h_req {
	union octep_ctrl_net_req_hdr hdr;
	union {
		struct octep_ctrl_net_f2h_req_cmd_state link;
	};
};

/* Fw to host response data */
struct octep_ctrl_net_f2h_resp {
	union octep_ctrl_net_resp_hdr hdr;
};

/* Max data size to be transferred over mbox */
union octep_ctrl_net_max_data {
	struct octep_ctrl_net_h2f_req h2f_req;
	struct octep_ctrl_net_h2f_resp h2f_resp;
	struct octep_ctrl_net_f2h_req f2h_req;
	struct octep_ctrl_net_f2h_resp f2h_resp;
};

#endif /* __OCTEP_CTRL_NET_H__ */

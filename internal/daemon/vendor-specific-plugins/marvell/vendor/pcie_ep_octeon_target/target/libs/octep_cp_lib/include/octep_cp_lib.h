/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __OCTEP_CP_LIB_H__
#define __OCTEP_CP_LIB_H__

#include <linux/vfio.h>

#ifndef BIT_ULL
#define BIT_ULL(nr) (1ULL << (nr))
#endif

#ifndef USE_PEM_AND_DPI_PF
#define USE_PEM_AND_DPI_PF 0
#endif

#define OCTEP_CP_VERSION(a, b, c)		(((a & 0xff) << 16) + \
						 ((b & 0xff) << 8) + \
						  (c & 0xff))

#define OCTEP_CP_DOM_MAX			8
#define OCTEP_CP_PF_PER_DOM_MAX			128
#define OCTEP_CP_MSG_DESC_MAX			4

#define OCTEP_CP_SOC_MODEL_CN96xx_A0		BIT_ULL(0)
#define OCTEP_CP_SOC_MODEL_CN96xx_B0		BIT_ULL(1)
#define OCTEP_CP_SOC_MODEL_CN96xx_C0		BIT_ULL(2)
#define OCTEP_CP_SOC_MODEL_CNF95xx_A0		BIT_ULL(4)
#define OCTEP_CP_SOC_MODEL_CNF95xx_B0		BIT_ULL(6)
#define OCTEP_CP_SOC_MODEL_CNF95xxMM_A0		BIT_ULL(8)
#define OCTEP_CP_SOC_MODEL_CNF95xxN_A0		BIT_ULL(12)
#define OCTEP_CP_SOC_MODEL_CNF95xxO_A0		BIT_ULL(13)
#define OCTEP_CP_SOC_MODEL_CNF95xxN_A1		BIT_ULL(14)
#define OCTEP_CP_SOC_MODEL_CNF95xxN_B0		BIT_ULL(15)
#define OCTEP_CP_SOC_MODEL_CN98xx_A0		BIT_ULL(16)
#define OCTEP_CP_SOC_MODEL_CN98xx_A1		BIT_ULL(17)
#define OCTEP_CP_SOC_MODEL_CN106xx_A0		BIT_ULL(20)
#define OCTEP_CP_SOC_MODEL_CNF105xx_A0		BIT_ULL(21)
#define OCTEP_CP_SOC_MODEL_CNF105xxN_A0		BIT_ULL(22)
#define OCTEP_CP_SOC_MODEL_CN103xx_A0		BIT_ULL(23)
#define OCTEP_CP_SOC_MODEL_CN106xx_A1		BIT_ULL(24)
#define OCTEP_CP_SOC_MODEL_CNF105xx_A1		BIT_ULL(25)
#define OCTEP_CP_SOC_MODEL_CNF105xxN_B0		BIT_ULL(26)
#define OCTEP_CP_SOC_MODEL_CN106xx_B0		BIT_ULL(27)

#define OCTEP_CP_SOC_MODEL_CN96xx_Ax		(OCTEP_CP_SOC_MODEL_CN96xx_A0 | \
						 OCTEP_CP_SOC_MODEL_CN96xx_B0)
#define OCTEP_CP_SOC_MODEL_CN98xx_Ax		(OCTEP_CP_SOC_MODEL_CN98xx_A0 | \
						 OCTEP_CP_SOC_MODEL_CN98xx_A1)
#define OCTEP_CP_SOC_MODEL_CN9K			(OCTEP_CP_SOC_MODEL_CN96xx_Ax | \
						 OCTEP_CP_SOC_MODEL_CN96xx_C0 | \
						 OCTEP_CP_SOC_MODEL_CNF95xx_A0 | \
						 OCTEP_CP_SOC_MODEL_CNF95xx_B0 | \
						 OCTEP_CP_SOC_MODEL_CNF95xxMM_A0 | \
						 OCTEP_CP_SOC_MODEL_CNF95xxO_A0 | \
						 OCTEP_CP_SOC_MODEL_CNF95xxN_A0 | \
						 OCTEP_CP_SOC_MODEL_CN98xx_Ax | \
						 OCTEP_CP_SOC_MODEL_CNF95xxN_A1 | \
						 OCTEP_CP_SOC_MODEL_CNF95xxN_B0)
#define OCTEP_CP_SOC_MODEL_CNF9K		(OCTEP_CP_SOC_MODEL_CNF95xx_A0 | \
						 OCTEP_CP_SOC_MODEL_CNF95xx_B0 | \
						 OCTEP_CP_SOC_MODEL_CNF95xxMM_A0 | \
						 OCTEP_CP_SOC_MODEL_CNF95xxO_A0 | \
						 OCTEP_CP_SOC_MODEL_CNF95xxN_A0 | \
						 OCTEP_CP_SOC_MODEL_CNF95xxN_A1 | \
						 OCTEP_CP_SOC_MODEL_CNF95xxN_B0)
#define OCTEP_CP_SOC_MODEL_CN106xx		(OCTEP_CP_SOC_MODEL_CN106xx_A0 | \
						 OCTEP_CP_SOC_MODEL_CN106xx_A1 | \
						 OCTEP_CP_SOC_MODEL_CN106xx_B0)
#define OCTEP_CP_SOC_MODEL_CNF105xx		(OCTEP_CP_SOC_MODEL_CNF105xx_A0 | \
						 OCTEP_CP_SOC_MODEL_CNF105xx_A1)
#define OCTEP_CP_SOC_MODEL_CNF105xxN		(OCTEP_CP_SOC_MODEL_CNF105xxN_A0 | \
						 OCTEP_CP_SOC_MODEL_CNF105xxN_B0)
#define OCTEP_CP_SOC_MODEL_CN103xx		(OCTEP_CP_SOC_MODEL_CN103xx_A0)
#define OCTEP_CP_SOC_MODEL_CN10K		(OCTEP_CP_SOC_MODEL_CN106xx | \
						 OCTEP_CP_SOC_MODEL_CNF105xx | \
						 OCTEP_CP_SOC_MODEL_CNF105xxN | \
						 OCTEP_CP_SOC_MODEL_CN103xx)
#define OCTEP_CP_SOC_MODEL_CNF10K		(OCTEP_CP_SOC_MODEL_CNF105xx | \
						 OCTEP_CP_SOC_MODEL_CNF105xxN)

#define OCTEP_CP_SOC_MODEL_STR_LEN_MAX		128

/* SoC model information */
struct octep_cp_soc_model {
	uint64_t flag;
	char name[OCTEP_CP_SOC_MODEL_STR_LEN_MAX];
};

/* Supported event types */
enum octep_cp_event_type {
	OCTEP_CP_EVENT_TYPE_INVALID,
	OCTEP_CP_EVENT_TYPE_PERST,	/* from host */
	OCTEP_CP_EVENT_TYPE_FLR,	/* from host */
	OCTEP_CP_EVENT_TYPE_FW_READY,	/* from app */
	OCTEP_CP_EVENT_TYPE_HEARTBEAT,	/* from app */
	OCTEP_CP_EVENT_TYPE_MAX
};

struct octep_cp_event_info_perst {
	/* index of pcie mac domain */
	int dom_idx;
};

struct octep_cp_event_info_flr {
	/* index of pcie mac domain */
	int dom_idx;
	/* index of pf in pcie mac domain */
	int pf_idx;
	/* map of vf indices in pf. 1 bit per vf */
	uint64_t vf_mask[2];
};

struct octep_cp_event_info_fw_ready {
	/* index of pcie mac domain */
	int dom_idx;
	/* index of pf in pcie mac domain */
	int pf_idx;
	/* firmware ready true/false */
	int ready;
};

struct octep_cp_event_info_heartbeat {
	/* index of pcie mac domain */
	int dom_idx;
	/* index of pf in pcie mac domain */
	int pf_idx;
};

/* library configuration */
struct octep_cp_event_info {
	enum octep_cp_event_type e;
	union {
		struct octep_cp_event_info_perst perst;
		struct octep_cp_event_info_flr flr;
		struct octep_cp_event_info_fw_ready fw_ready;
		struct octep_cp_event_info_heartbeat hbeat;
	} u;
};

/* Information for sending messages */
union octep_cp_msg_info {
	uint64_t words[2];
	struct {
		uint16_t pem_idx:4;
		/* sender pf index 0-(n-1) */
		uint16_t pf_idx:9;
		uint16_t reserved:2;
		uint16_t is_vf:1;
		/* sender vf index 0-(n-1) */
		uint16_t vf_idx;
		/* message size */
		uint32_t sz;
		/* reserved */
		uint64_t reserved1;
	} s;
};

/* Message buffer */
struct octep_cp_msg_buf {
	uint32_t reserved1;
	uint16_t reserved2;
	/* size of buffer */
	uint16_t sz;
	/* pointer to message buffer */
	void *msg;
};

/* Message */
struct octep_cp_msg {
	/* Message info */
	union octep_cp_msg_info info;
	/* number of sg buffer's */
	int sg_num;
	/* message buffer's */
	struct octep_cp_msg_buf sg_list[OCTEP_CP_MSG_DESC_MAX];
};

/* pcie mac domain pf configuration */
struct octep_cp_pf_cfg {
	/* pcie mac domain pf index */
	int idx;
	/* Maximum supported message size to be filled by library */
	uint16_t max_msg_sz;
};

/* pcie mac domain configuration */
struct octep_cp_dom_cfg {
	/* pcie mac domain index */
	int idx;
	/* pf count */
	uint16_t npfs;
	/* pf indices */
	struct octep_cp_pf_cfg pfs[OCTEP_CP_PF_PER_DOM_MAX];
};

/* library configuration */
struct octep_cp_lib_cfg {
	/* Info to be filled by caller */
	/* Control plane min supported version,
	 * should be of type OCTEP_CP_VERSION
	 */
	uint32_t min_version;
	/* Control plane max supported version,
	 * should be of type OCTEP_CP_VERSION
	 */
	uint32_t max_version;
	/* number of pcie mac domains */
	uint16_t ndoms;
	/* configuration for pcie mac domains */
	struct octep_cp_dom_cfg doms[OCTEP_CP_DOM_MAX];
};

/* pcie mac domain pf information */
struct octep_cp_pf_info {
	/* pcie mac domain pf index */
	int idx;
	/* Maximum supported message size */
	uint16_t max_msg_sz;
	/* Host control plane version of type OCTEP_CP_VERSION */
	uint32_t host_version;
};

/* pcie mac domain information */
struct octep_cp_dom_info {
	/* pcie mac domain index */
	int idx;
	/* pf count */
	uint16_t npfs;
	/* pf information */
	struct octep_cp_pf_info pfs[OCTEP_CP_PF_PER_DOM_MAX];
};

/* library information */
struct octep_cp_lib_info {
	/* Detected soc */
	struct octep_cp_soc_model soc_model;
	/* number of pcie mac domains */
	uint16_t ndoms;
	/* configuration for pcie mac domains */
	struct octep_cp_dom_info doms[OCTEP_CP_DOM_MAX];
};

/* Parse command line arguments for octep_cp library */
int octep_cp_lib_parse_args(int argc, char **argv, struct octep_cp_lib_cfg *cfg);

/* Initialize octep_cp library.
 *
 * Library will fill in information after initialization.
 *
 * @param cfg: [IN/OUT] non-null pointer to struct octep_cp_lib_cfg.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_cp_lib_init(struct octep_cp_lib_cfg *cfg);

/* Initialize a pem after a perst or other reset operation
 *
 * Library will fill in information of pem after initialization.
 *
 * @param cfg: [IN/OUT] non-null pointer to struct octep_cp_lib_cfg.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_cp_lib_init_pem(struct octep_cp_lib_cfg *cfg, int dom_idx);

/* Get library information after initialization.
 *
 * This api will return valid information only after library is initialized.
 * pf host version is available after the host is initialized. pf host version is
 * retrieved when available in octep_cp_lib_recv_msg api. So if a valid pf host
 * version is required then get_info api should be called after receiving first
 * message from host.
 *
 * @param info: [IN/OUT] non-null pointer to struct octep_cp_lib_info.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_cp_lib_get_info(struct octep_cp_lib_info *info);

/* Send response to received message.
 *
 * Total buffer size cannot exceed max_msg_sz in library configuration.
 *
 * @param ctx: [IN] non-null pointer to union octep_cp_msg_info. This will
 *             provide the pem, pf indices on which the message should be
 *             sent.
 * @param msg: [IN] Array of non-null pointer to message.
 * @param num: [IN] Number of elements in @msg.
 *
 * return value: number of messages sent on success, -errno on failure.
 */
int octep_cp_lib_send_msg_resp(union octep_cp_msg_info *ctx,
			       struct octep_cp_msg *msg,
			       int num);

/* Send a new notification.
 *
 * Reply is not expected for this message.
 * Buffer size cannot exceed max_msg_sz in library configuration.
 *
 * @param ctx: [IN] non-null pointer to union octep_cp_msg_info. This will
 *             provide the pem, pf indices on which the message should be
 *             sent.
 * @param msg: [IN] Message buffer.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_cp_lib_send_notification(union octep_cp_msg_info *ctx,
				   struct octep_cp_msg* msg);

/* Receive a new message on given pem/pf.
 *
 * ctx received with the message should be used to send a response.
 *
 * @param ctx: [IN] non-null pointer to union octep_cp_msg_info. This will
 *             provide the pem, pf indices on which the message should be
 *             sent.
 * @param msg: [IN/OUT] Array of non-null pointer to message.
 *             Caller should provide msg.sz, msg.sg_list[*].sz.
 * @param num: Number of elements in @msg.
 *
 * return value: number of messages received on success, -errno on failure.
 */
int octep_cp_lib_recv_msg(union octep_cp_msg_info *ctx,
			  struct octep_cp_msg *msg,
			  int num);

/* Send event to host.
 *
 * Send a new event to host.
 *
 * @param info: [IN] Non-Null pointer to event info structure.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_cp_lib_send_event(struct octep_cp_event_info *info);

/* Receive events.
 *
 * Receive events such as flr, perst etc.
 *
 * @param info: [OUT] Non-Null pointer to event info array.
 * @param num: [IN] Number of event info buffers.
 *
 * return value: number of events received on success, -errno on failure.
 */
int octep_cp_lib_recv_event(struct octep_cp_event_info *info, int num);

/* Uninitialize lib values for a pem
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_cp_lib_uninit_pem(int dom_idx);

/* Uninitialize cp library.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_cp_lib_uninit();
#if USE_PEM_AND_DPI_PF
int cnxk_vfio_parse_dpi_dev(const char *dev);
int cnxk_vfio_parse_pem_dev(const char *dev);
uint64_t cnxk_pem_get_mbox_memory(int pem);
#endif

#endif /* __OCTEP_CP_LIB_H__ */

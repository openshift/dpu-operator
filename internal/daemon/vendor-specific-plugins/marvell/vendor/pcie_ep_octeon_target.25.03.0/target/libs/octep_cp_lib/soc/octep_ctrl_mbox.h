/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __OCTEP_CTRL_MBOX_H__
#define __OCTEP_CTRL_MBOX_H__

/*              barmem structure
 * |===========================================|
 * |Info (16 + 120 + 120 = 256 bytes)          |
 * |-------------------------------------------|
 * |magic number (8 bytes)                     |
 * |bar memory size (4 bytes)                  |
 * |reserved (4 bytes)                         |
 * |-------------------------------------------|
 * |host version (8 bytes)                     |
 * |    low 32 bits                            |
 * |host status (8 bytes)                      |
 * |host reserved (104 bytes)                  |
 * |-------------------------------------------|
 * |fw version's (8 bytes)                     |
 * |    min=high 32 bits, max=low 32 bits      |
 * |fw status (8 bytes)                        |
 * |fw reserved (104 bytes)                    |
 * |===========================================|
 * |Host to Fw Queue info (16 bytes)           |
 * |-------------------------------------------|
 * |producer index (4 bytes)                   |
 * |consumer index (4 bytes)                   |
 * |max msg size (4 bytes)                     |
 * |reserved (4 bytes)                         |
 * |===========================================|
 * |Fw to Host Queue info (16 bytes)           |
 * |-------------------------------------------|
 * |producer index (4 bytes)                   |
 * |consumer index (4 bytes)                   |
 * |max msg size (4 bytes)                     |
 * |reserved (4 bytes)                         |
 * |===========================================|
 * |Host to Fw Queue ((total size-288/2) bytes)|
 * |-------------------------------------------|
 * |                                           |
 * |===========================================|
 * |===========================================|
 * |Fw to Host Queue ((total size-288/2) bytes)|
 * |-------------------------------------------|
 * |                                           |
 * |===========================================|
 */

#ifndef BIT
#define BIT(a)	(1ULL << (a))
#endif

#define OCTEP_CTRL_MBOX_MAGIC_NUMBER		0xdeaddeadbeefbeefull

/* Valid request message */
#define OCTEP_CTRL_MBOX_MSG_HDR_FLAG_REQ	BIT(0)
/* Valid response message */
#define OCTEP_CTRL_MBOX_MSG_HDR_FLAG_RESP	BIT(1)
/* Valid notification, no response required */
#define OCTEP_CTRL_MBOX_MSG_HDR_FLAG_NOTIFY	BIT(2)
/* Valid custom message */
#define OCTEP_CTRL_MBOX_MSG_HDR_FLAG_CUSTOM	BIT(3)

#define OCTEP_CTRL_MBOX_MSG_DESC_MAX		4

enum octep_ctrl_mbox_status {
	OCTEP_CTRL_MBOX_STATUS_INVALID = 0,
	OCTEP_CTRL_MBOX_STATUS_INIT,
	OCTEP_CTRL_MBOX_STATUS_READY,
	OCTEP_CTRL_MBOX_STATUS_UNINIT
};

/* mbox message */
union octep_ctrl_mbox_msg_hdr {
	uint64_t words[2];
	struct {
		uint16_t pem_idx:4;
		uint16_t pf_idx:9;
		uint16_t reserved:2;
		/* vf_idx is valid if 1 */
		uint16_t is_vf:1;
		/* sender vf index 0-(n-1), 0 if (is_vf==0) */
		uint16_t vf_idx;
		/* total size of message excluding header */
		uint32_t sz;
		/* OCTEP_CTRL_MBOX_MSG_HDR_FLAG_* */
		uint32_t flags;
		/* identifier to match responses */
		uint16_t msg_id;
		uint16_t reserved2;
	} s;
};

/* mbox message buffer */
struct octep_ctrl_mbox_msg_buf {
	uint32_t reserved1;
	uint16_t reserved2;
	/* size of buffer */
	uint16_t sz;
	/* pointer to message buffer */
	void *msg;
};

/* mbox message */
struct octep_ctrl_mbox_msg {
	/* mbox transaction header */
	union octep_ctrl_mbox_msg_hdr hdr;
	/* number of sg buffer's */
	int sg_num;
	/* message buffer's */
	struct octep_ctrl_mbox_msg_buf sg_list[OCTEP_CTRL_MBOX_MSG_DESC_MAX];
};

/* Mbox queue */
struct octep_ctrl_mbox_q {
	/* size of queue buffer */
	uint32_t sz;
	/* producer address in bar mem */
	uint64_t hw_prod;
	/* consumer address in bar mem */
	uint64_t hw_cons;
	/* q base adddress in bar mem */
	uint64_t hw_q;
};

struct octep_ctrl_mbox {
	/* Control plane min supported version,
	 * should be of type OCTEP_CP_VERSION
	 */
	uint32_t min_version;
	/* Control plane max supported version,
	 * should be of type OCTEP_CP_VERSION
	 */
	uint32_t max_version;
	/* size of bar memory */
	uint32_t barmem_sz;
	/* pointer to BAR memory */
	uint64_t barmem;
	/* host-to-fw queue */
	struct octep_ctrl_mbox_q h2fq;
	/* fw-to-host queue */
	struct octep_ctrl_mbox_q f2hq;
	/* file descriptor for bar memory */
	int bar4_fd;
	/* host version */
	uint64_t host_version;
};

/* Initialize control mbox.
 *
 * @param mbox: non-null pointer to struct octep_ctrl_mbox.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_ctrl_mbox_init(struct octep_ctrl_mbox *mbox);

/* Send mbox message.
 *
 * @param mbox: non-null pointer to struct octep_ctrl_mbox.
 * @param msgs: Array of non-null pointers to struct octep_ctrl_mbox_msg.
 *             Caller should fill msg.sz and msg.desc.sz for each message.
 * @param num: Size of msg array.
 *
 * return value: number of messages sent on success, -errno on failure.
 */
int octep_ctrl_mbox_send(struct octep_ctrl_mbox *mbox,
			 struct octep_ctrl_mbox_msg *msgs,
			 int num);

/* Retrieve mbox message.
 *
 * @param mbox: non-null pointer to struct octep_ctrl_mbox.
 * @param msgs: Array of non-null pointers to struct octep_ctrl_mbox_msg.
 *             Caller should fill msg.sz and msg.desc.sz for each message.
 * @param num: Size of msg array.
 *
 * return value: number of messages received on success, -errno on failure.
 */
int octep_ctrl_mbox_recv(struct octep_ctrl_mbox *mbox,
			 struct octep_ctrl_mbox_msg *msgs,
			 int num);

/* Uninitialize control mbox.
 *
 * @param mbox: non-null pointer to struct octep_ctrl_mbox.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_ctrl_mbox_uninit(struct octep_ctrl_mbox *mbox);

#endif /* __OCTEP_CTRL_MBOX_H__ */

/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __CNXK_H__
#define __CNXK_H__

/* Initialize cnxk platform.
 *
 * return value: 0 on success, -errno on failure.
 */
int cnxk_init(struct octep_cp_lib_cfg *cfg);

/* Initialize a particular pem
 *
 * return value: 0 on success, -errno on failure.
 */
int cnxk_init_pem(struct octep_cp_lib_cfg *cfg, int dom_idx);

/* Get platform information after initialization.
 *
 * Fill in information after initialization.
 *
 * @param info: [IN/OUT] non-null pointer to struct octep_cp_lib_info.
 *
 * return value: 0 on success, -errno on failure.
 */
int cnxk_get_info(struct octep_cp_lib_info *info);

/* Send response to received message.
 *
 * Total buffer size cannot exceed max_msg_sz in library configuration.
 *
 * @param ctx: [IN] non-null pointer to union octep_cp_msg_info. This will
 *             provide the pem, pf indices on which the message should be
 *             sent.
 * @param msgs: [IN] Array of non-null pointer to message.
 * @param num: [IN] Number of elements in @msgs.
 *
 * return value: number of messages sent on success, -errno on failure.
 */
int cnxk_send_msg_resp(union octep_cp_msg_info *ctx,
                       struct octep_cp_msg *msgs,
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
int cnxk_send_notification(union octep_cp_msg_info *ctx,
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
int cnxk_recv_msg(union octep_cp_msg_info *ctx,
                  struct octep_cp_msg *msgs,
                  int num);

/* Send event to host.
 *
 * Send a new event to host.
 *
 * @param info: [IN] Non-Null pointer to event info structure.
 *
 * return value: 0 on success, -errno on failure.
 */
int cnxk_send_event(struct octep_cp_event_info *info);

/* Receive events.
 *
 * Receive events such as flr, perst etc.
 *
 * @param info: [OUT] Non-Null pointer to event info array.
 * @param num: [IN] Number of event info buffers.
 *
 * return value: number of events received on success, -errno on failure.
 */
int cnxk_recv_event(struct octep_cp_event_info *info, int num);

/* UnInitialize cnxk mbox, csr's etc for a pem.
 *
 * return value: 0 on success, -errno on failure.
 */
int cnxk_uninit_pem(int dom_idx);

/* UnInitialize cnxk mbox, csr's etc.
 *
 * return value: 0 on success, -errno on failure.
 */
int cnxk_uninit();

#if USE_PEM_AND_DPI_PF
int cnxk_vfio_global_init(void);
void cnxk_vfio_global_uninit(void);
int cnxk_pem_init(int pem);
void cnxk_pem_uninit(int pem);
void *cnxk_pem_map_reg(int pem_idx, unsigned long long addr);
int cnxk_check_perst_intr(int pem);
int cnxk_clear_perst_intr(int pem);

extern struct octep_cp_lib_cfg *lib_cfg;
#endif
#endif /* __CNXK_H__ */

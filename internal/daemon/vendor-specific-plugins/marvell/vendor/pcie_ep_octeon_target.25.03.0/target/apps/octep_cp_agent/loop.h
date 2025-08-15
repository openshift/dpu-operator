/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __LOOP_H__
#define __LOOP_H__

/* Initialize loop mode implementation.
 *
 * return value: 0 on success, -errno on failure.
 */
int loop_init(int max_msgs);

/* Initialize loop mode implementation for a pem
 *
 * return value: 0 on success, -errno on failure.
 */
int loop_init_pem(int dom_idx);

/* Process interrupts and host messages.
 *
 * return value: size of response in words on success, -errno on failure.
 */
int loop_process_msgs();

/* Process user interrupt signal.
 *
 * return value: 0 on success, -errno on failure.
 */
int loop_process_sigusr1();

/* Uninitialize loop mode implementation.
 *
 * return value: 0 on success, -errno on failure.
 */
int loop_uninit();

/* Uninitialize loop mode implementation for a pem
 *
 * return value: 0 on success, -errno on failure.
 */
int loop_uninit_pem(int dom_idx);

#endif /* __LOOP_H__ */

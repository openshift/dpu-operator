/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __OCTEP_PLUGIN_CLIENT_H__
#define __OCTEP_PLUGIN_CLIENT_H__

#ifdef __cplusplus
extern "C" {
#endif

#include "octep_plugin_common.h"

#define OCTEP_PLUGIN_CLIENT_MAX_DEVICES			128

/* Operating state of plugin client */
enum {
	OCTEP_PLUGIN_CLIENT_STATE_INVALID,
	OCTEP_PLUGIN_CLIENT_STATE_INIT,
	OCTEP_PLUGIN_CLIENT_STATE_CONNECTED,
	OCTEP_PLUGIN_CLIENT_STATE_MAX
};

struct octep_plugin_client_info {
	int state;
	int client_sockfd;
	int num_devs;
	struct octep_plugin_info *info;
	struct octep_plugin_dev_id dev_list[OCTEP_PLUGIN_CLIENT_MAX_DEVICES];
};

extern uint32_t octep_plugin_client_host_version[OCTEP_PLUGIN_MAX_PEM]
					 [OCTEP_PLUGIN_MAX_PF_PER_PEM];

/* Initialize plugin client.
 *
 * @param info: Non-null pointer containing plugin information.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_plugin_client_init(struct octep_plugin_info *info);

/* Start plugin client.
 *
 * Initiate asynchronous connection to plugin server. Client state will move
 * to OCTEP_PLUGIN_CLIENT_STATE_CONNECTED on success.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_plugin_client_start(void);

/* Register a device handler with plugin server.
 *
 * Client needs to be in OCTEP_PLUGIN_CLIENT_STATE_CONNECTED state for this api
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_plugin_client_dev_register(struct octep_plugin_dev_id *id);

/* Unregister a device handler with plugin server.
 *
 * If id == NULL then unregister all currently registered devices.
 * Client needs to be in OCTEP_PLUGIN_CLIENT_STATE_CONNECTED state for this api
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_plugin_client_dev_unregister(struct octep_plugin_dev_id *id);

/* Send a notification to plugin server.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_plugin_client_send_notification(struct octep_plugin_msg *msg);

/* Poll plugin client socket for valid ctrl net msgs.
 *
 * return value: Number of bytes read on success, -errno on failure.
 */
int octep_plugin_client_poll(struct octep_plugin_msg *msg);

/* Stop plugin client.
 *
 * Disconnect from plugin server and cleanup any connection related data.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_plugin_client_stop(void);

/* Get current operating state.
 *
 * @param state: Non-null pointer in which current state will be copied.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_plugin_client_get_state(uint32_t *state);

/* Uninitialize plugin client.
 *
 * return value: 0 on success, -errno on failure.
 */
int octep_plugin_client_uninit(void);

#ifdef __cplusplus
}
#endif

#endif /* __OCTEP_PLUGIN_CLIENT_H__ */

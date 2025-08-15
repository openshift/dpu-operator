/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __OCTEP_PLUGIN_SERVER_H__
#define __OCTEP_PLUGIN_SERVER_H__

#ifdef __cplusplus
extern "C" {
#endif

#include <pthread.h>
#include <stdbool.h>
#include "octep_plugin_server_config.h"
#include "octep_plugin_common.h"

#define OCTEP_PLUGIN_SERVER_DEBUG		0
#define OCTEP_PLUGIN_INVALID_CLIENT_ID		(0xFFFF)
#define OCTEP_PLUGIN_INVALID_CLIENT_SOCKFD	0

#ifdef	OCTEP_PLUGIN_SERVER_DEBUG

#define	PLUGIN_CLIENT_MSG_DUMP		1

#else

/* Default values for individual debugging */
#define	PLUGIN_CLIENT_MSG_DUMP		0

#endif

/* Structure for plugin server info */
typedef struct octep_plugin_server {
	pthread_mutex_t ctrl_net_lock;
} octep_plugin_server_t;

/* Plugin client operating states */
enum plugin_client_state {
	OCTEP_PLUGIN_CLIENT_STATE_INVAL = 0,
	OCTEP_PLUGIN_CLIENT_STATE_CONNECTED,
	OCTEP_PLUGIN_CLIENT_STATE_INIT,
	OCTEP_PLUGIN_CLIENT_STATE_REGD,
};

/* Structure represnting a plugin client */
struct plugin_client_app {
	int client_id;
	int num_devs;
	int state;
	int sockfd;
};

/* Array to store pem::pf host versions */
extern uint32_t octep_plugin_server_host_version[OCTEP_PLUGIN_MAX_PEM][OCTEP_PLUGIN_MAX_PF_PER_PEM];

/*
 * Initialise server socket and start octep_plugin_server_loop thread
 *
 * @param: struct plugin_app_cfg *app_cfg containing plugin information
 *         about interfaces.
 *
 * return: (int) 0 on success, -errno on failure
 */
int octep_plugin_server_init(struct plugin_app_cfg *app_cfg);

/*
 * Lock mutex for control net access functions
 *
 * @param: void
 *
 * return: (int) 0 on success, -errno on failure
 */
int octep_plugin_server_ctrl_net_lock(void);

/*
 * Host request handler for plugin server. Msg will be forwarded to client
 * if request is to valid plugin controlled interface and valid client.
 *
 * @param: struct octep_cp_msg *msg
 * return: (int) 0 on success, -errno on failure
 */
int octep_plugin_server_process_msg(struct octep_cp_msg *msg);

/*
 * Event handler for plugin server. Forwards event info to connected client apps
 *
 * @param: struct octep_cp_event_info *event
 *
 * return: void
 */
void octep_plugin_server_process_event(struct octep_cp_event_info *event);

/*
 * Send host version of pem::pf to any related connected client.
 *
 * @param: uint16_t pem, uint16_t pf, uint32_t host_version
 *
 * return: void
 */
void octep_plugin_server_relay_host_version(uint16_t pem, uint16_t pf, uint32_t host_vers);

/*
 * Unlock mutex for control net access functions
 *
 * @param: void
 *
 * return: (int) 0 on success, -errno on failure
 */
int octep_plugin_server_ctrl_net_unlock(void);

/*
 * Uninitialises plugin server. Cancels octep_plugin_server_loop and destroys
 * control net access mutex lock.
 *
 * @param: void
 *
 * return: void
 */
void octep_plugin_server_uninit(void);

/**************************
 ****** Debug apis*********
 **************************/

/*
 * Dump octep_plugin_msg_hdr for given octep_plugin_msg
 *
 * @param: struct octep_plugin_msg *msg
 *
 * return: void
 */
void octep_plugin_client_msg_hdr_dump(struct octep_plugin_msg *msg);

/*
 * Dump data from given octep_plugin_msg.
 *
 * @param: struct octep_plugin_msg *msg, bool is_cp_msg
 *
 * return: void
 */
void octep_plugin_client_msg_data_dump(struct octep_plugin_msg *msg, bool is_cp_msg);

#ifdef __cplusplus
}
#endif

#endif /* __OCTEP_PLUGIN_SERVER_H__ */

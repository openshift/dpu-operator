// SPDX-License-Identifier: BSD-3-Clause
/* Copyright (c) 2022 Marvell.
 */
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <errno.h>
#include <arpa/inet.h>
#include <pthread.h>
#include <string.h>

#include "octep_cp_lib.h"
#include "octep_ctrl_net.h"
#include "octep_plugin_server.h"
#include "octep_plugin_server_config.h"

#define PLUGIN_VERSION_MAJOR		1
#define PLUGIN_VERSION_MINOR		1
#define PLUGIN_VERSION_VARIANT		0

#define OCTEP_PLUGIN_SERVER_VERSION	(OCTEP_PLUGIN_VERSION(PLUGIN_VERSION_MAJOR, \
							      PLUGIN_VERSION_MINOR, \
							      PLUGIN_VERSION_VARIANT))

static octep_plugin_server_t plugin_server;
static fd_set plugin_client_fdset;
static struct plugin_app_cfg cfg;
static pthread_t process_thread;
static int server_sockfd;
static struct plugin_client_app plugin_client[OCTEP_PLUGIN_MAX_CLIENTS] = {
	[0 ... OCTEP_PLUGIN_MAX_CLIENTS - 1].client_id = OCTEP_PLUGIN_INVALID_CLIENT_ID,
	[0 ... OCTEP_PLUGIN_MAX_CLIENTS - 1].num_devs = 0,
	[0 ... OCTEP_PLUGIN_MAX_CLIENTS - 1].state = OCTEP_PLUGIN_CLIENT_STATE_INVAL,
	[0 ... OCTEP_PLUGIN_MAX_CLIENTS - 1].sockfd = OCTEP_PLUGIN_INVALID_CLIENT_SOCKFD
};

static inline void *get_in_addr(struct sockaddr *sa)
{
	return (sa->sa_family == AF_INET) ?
	       (void *)(&(((struct sockaddr_in *) sa)->sin_addr)) :
	       (void *)(&(((struct sockaddr_in6 *) sa)->sin6_addr));
}

__attribute__((visibility("default")))
uint32_t octep_plugin_server_host_version[OCTEP_PLUGIN_MAX_PEM]
					 [OCTEP_PLUGIN_MAX_PF_PER_PEM] = { 0 };

/*
 * Prepare octep_cp_msg_info context from given octep_plugin_dev_id
 *
 * The context can be used to obtain the interface specific plugin_fn_cfg
 * data structure
 *
 * @param: [IN/OUT] union octep_cp_msg_info *ctx, [IN] struct octep_plugin_dev_id *dev
 *
 * return: void
 */
static void plugin_context_prep(union octep_cp_msg_info *ctx, struct octep_plugin_dev_id *dev)
{
	ctx->s.pem_idx = dev->pem;
	ctx->s.pf_idx = dev->pf;
	dev->vf == OCTEP_PLUGIN_INVALID_VF_IDX ? (ctx->s.is_vf = 0) : (ctx->s.is_vf = 1);
	ctx->s.vf_idx = dev->vf;
}

/*
 * Forward request from host to plugin client app.
 *
 * @param: [IN] int sockfd, [IN] struct octep_plugin_msg *msg
 *
 * return: (int) 0 on success, -errno on error
 */
static int plugin_fwd_to_app(int sockfd, struct octep_plugin_msg *msg)
{
	struct octep_cp_msg *cp_msg;
	int ret, i, total_sz = 0;
	uint8_t *buf;

	if (msg->hdr.id == OCTEP_PLUGIN_S2C_MSG_HOST_VERSION)
		goto sock_send;

	cp_msg = (struct octep_cp_msg *) &msg->data;

	buf = &msg->data[msg->hdr.sz];
	for (i = 0; i < cp_msg->sg_num; i++) {
		memcpy(buf, cp_msg->sg_list[i].msg, cp_msg->sg_list[i].sz);
		buf += cp_msg->sg_list[i].sz;
		total_sz += cp_msg->sg_list[i].sz;
	}

sock_send:
	ret = send(sockfd, msg, msg->hdr.sz + sizeof(msg->hdr) + total_sz, 0);
	if (ret != (msg->hdr.sz + sizeof(msg->hdr) + total_sz)) {
		printf("PLUGIN_SERVER: Send error: Could only send %d bytes out of %ld total bytes to app\n",
		       ret, sizeof(*msg));
		return -EIO;
	}

	return 0;
}

/*
 * Internal api to push host version to client apps.
 * force_sockfd param decides whether host version should be
 * pushed to given connection or do default behaviour, which is
 * update for all client apps with state > INIT, iff new state is
 * different from existing state.
 *
 * @param: [IN] uint16_t pem, [IN] uint16_t pf, [IN] uint32_t host_vers,
 *	   [IN] int force_sockfd
 *
 * return: void
 */
static void octep_plugin_server_relay_force_host_version(uint16_t pem, uint16_t pf,
							 uint32_t host_vers, int force_sockfd)
{
	struct octep_plugin_msg msg = { 0 };
	int i, sockfd;

	if (octep_plugin_server_host_version[pem][pf] == host_vers && !force_sockfd)
		return;

	msg.hdr.dev_id.pem = pem;
	msg.hdr.dev_id.pf = pf;
	msg.hdr.dev_id.vf = OCTEP_PLUGIN_INVALID_VF_IDX;

	/* When a new plugin client app inits, host version needs to
	 * be forcefully relayed despite no change in version.
	 * In that case, just send it to the new connection explicitly.
	 */
	if (force_sockfd) {
		msg.hdr.id = OCTEP_PLUGIN_S2C_MSG_HOST_VERSION;
		msg.hdr.sz = sizeof(uint32_t);
		(*(uint32_t *) &msg.data) = host_vers;

		plugin_fwd_to_app(force_sockfd, &msg);
		return;
	}

	/* Send host version info to all plugin clients
	 * with valid state.
	 */
	for (i = 0; i < OCTEP_PLUGIN_MAX_CLIENTS; i++) {
		if (plugin_client[i].state < OCTEP_PLUGIN_CLIENT_STATE_INIT)
			continue;

		sockfd = plugin_client[i].sockfd;
		msg.hdr.id = OCTEP_PLUGIN_S2C_MSG_HOST_VERSION;
		msg.hdr.sz = sizeof(uint32_t);
		(*(uint32_t *) &msg.data) = host_vers;

		plugin_fwd_to_app(sockfd, &msg);
	}
	octep_plugin_server_host_version[pem][pf] = host_vers;
}

/* Internal api to send valid/invalid response to client
 *
 * @param: [IN] int sockfd, [IN/OUT] struct struct octep_plugin_msg *msg,
 *	   [IN] bool valid
 *
 * return: void
 */
static void plugin_send_response(int sockfd, struct octep_plugin_msg *msg, bool valid)
{
	if (valid)
		msg->hdr.id = OCTEP_PLUGIN_S2C_MSG_PLUGIN_RESP;
	else
		msg->hdr.id = OCTEP_PLUGIN_S2C_MSG_INVALID;

	send(sockfd, msg, msg->hdr.sz + sizeof(msg->hdr), 0);
}

/* Internal api to loop and send all pem::pf host versions
 * to a newly inited client app
 *
 * @param: int sockfd
 *
 * return: void
 */
static void plugin_server_relay_host_version_init(int sockfd)
{
	struct octep_plugin_msg msg;
	int pem, pf;

	for (pem = 0; pem < OCTEP_PLUGIN_MAX_PEM; pem++)
		for (pf = 0; pf < OCTEP_PLUGIN_MAX_PF_PER_PEM; pf++) {
			/* For new clients, the data structure will be zeroed
			 * out anyway on the client side.
			 */
			if (octep_plugin_server_host_version[pem][pf] == 0)
				continue;

			octep_plugin_server_relay_force_host_version
				(pem, pf, octep_plugin_server_host_version[pem][pf],
				 sockfd);
		}

	/* Send end of message */
	msg.hdr.sz = sizeof(int);
	plugin_send_response(sockfd, &msg, true);
}

/*
 * Handle messages from plugin client apps directed to plugin server
 *
 * @param: [IN] struct plugin_client_app *client, [IN] struct octep_plugin_msg *msg
 *
 * return: void
 */
static void plugin_handle_client_msg(struct plugin_client_app *client, struct octep_plugin_msg *msg)
{
	union octep_cp_msg_info ctx = { 0 };
	struct plugin_fn_cfg *fn;

	switch (msg->hdr.id) {
	case OCTEP_PLUGIN_C2S_MSG_INIT:
		if ((*(uint32_t *) &msg->data) == OCTEP_PLUGIN_SERVER_VERSION) {
			client->state = OCTEP_PLUGIN_CLIENT_STATE_INIT;
			plugin_send_response(client->sockfd, msg, true);
			plugin_server_relay_host_version_init(client->sockfd);
		} else {
			plugin_send_response(client->sockfd, msg, false);
		}

		break;
	case OCTEP_PLUGIN_C2S_MSG_DEV_REGISTER:
		if (client->state < OCTEP_PLUGIN_CLIENT_STATE_INIT) {
			printf("PLUGIN_SERVER: Client %d has not initialised yet to register\n",
			       client->client_id);
			plugin_send_response(client->sockfd, msg, false);
			break;
		}

		plugin_context_prep(&ctx, &msg->hdr.dev_id);
		fn = plugin_app_config_get_fn(&cfg, &ctx);

		/* Check if interface is plugin controlled and is not owned by a client yet
		 * If not owned yet, the client_id will be OCTEP_PLUGIN_INVALID_CLIENT_ID
		 */
		if (fn->plugin_controlled && fn->client_id == OCTEP_PLUGIN_INVALID_CLIENT_ID) {
			fn->client_id = client->client_id;
			client->state = OCTEP_PLUGIN_CLIENT_STATE_REGD;
			client->num_devs++;
			plugin_send_response(client->sockfd, msg, true);
		} else {
			printf("PLUGIN_SERVER: Invalid interface requested by client\n");
			plugin_send_response(client->sockfd, msg, false);
		}

		break;
	case OCTEP_PLUGIN_C2S_MSG_DEV_UNREGISTER:
		if (client->state < OCTEP_PLUGIN_CLIENT_STATE_REGD) {
			printf("PLUGIN_SERVER: Client %d has not registered any device yet\n",
			       client->client_id);
			return;
		}

		plugin_context_prep(&ctx, &msg->hdr.dev_id);
		fn = plugin_app_config_get_fn(&cfg, &ctx);
		if (!fn->plugin_controlled || fn->client_id != client->client_id) {
			printf("PLUGIN_SERVER: Client %d sent unregister for invalid interface\n",
			       client->client_id);
			return;
		}
		fn->client_id = OCTEP_PLUGIN_INVALID_CLIENT_ID;
		client->num_devs--;

		if (client->num_devs == 0)
			client->state = OCTEP_PLUGIN_CLIENT_STATE_INIT;
		break;
	default:
		printf("PLUGIN_SERVER: Invalid request to plugin server from client %d\n",
		       client->client_id);
		plugin_send_response(client->sockfd, msg, false);
		break;
	};
}

/*
 * Forward messages from plugin client apps to corresponding host interfaces
 *
 * @param: [IN] struct plugin_client_app *client, [IN] struct octep_plugin_msg *msg
 *
 * return: void
 */
static void plugin_fwd_to_host(struct plugin_client_app *client, struct octep_plugin_msg *msg)
{
	union octep_cp_msg_info ctx = { 0 };
	struct octep_cp_msg *cp_msg;
	struct plugin_fn_cfg *fn;
	uint8_t *buf;
	int i, ret;

	plugin_context_prep(&ctx, &msg->hdr.dev_id);
	fn = plugin_app_config_get_fn(&cfg, &ctx);

	if (client->state != OCTEP_PLUGIN_CLIENT_STATE_REGD) {
		printf("PLUGIN_SERVER: Client %d has not registered to any valid interface yet\n",
				client->client_id);
		return;
	} else if (fn->client_id != client->client_id) {
		printf("PLUGIN_SERVER: Client %d trying to send ctrl_net to unregistered interface\n",
				client->client_id);
		return;
	}

	cp_msg = (struct octep_cp_msg *) &msg->data;
	buf = &msg->data[msg->hdr.sz];
	for (i = 0; i < cp_msg->sg_num; i++) {
		ret = read(client->sockfd, buf, cp_msg->sg_list[i].sz);
		if (ret != cp_msg->sg_list[i].sz) {
			printf("PLUGIN_SERVER: Incomplete "
					"sg received\n");
			continue;
		}
		cp_msg->sg_list[i].msg = buf;
		buf += cp_msg->sg_list[i].sz;
	}

	switch (msg->hdr.id) {
	case OCTEP_PLUGIN_C2S_MSG_CTRL_NET_NOTIFY:
		octep_plugin_server_ctrl_net_lock();
		ret = octep_cp_lib_send_notification(&ctx, (struct octep_cp_msg *) &msg->data);
		if (ret < 0)
			printf("PLUGIN_SERVER: Notification fwd to host failed with err %d\n",
			       ret);
		octep_plugin_server_ctrl_net_unlock();
		break;
	case OCTEP_PLUGIN_C2S_MSG_CTRL_NET_RESP:
		octep_plugin_server_ctrl_net_lock();
		ret = octep_cp_lib_send_msg_resp(&ctx, (struct octep_cp_msg *) &msg->data, 1);
		if (ret < 0)
			printf("PLUGIN_SERVER: Response fwd to host failed with err %d\n",
			       ret);
		octep_plugin_server_ctrl_net_unlock();
		break;
	default:
		printf("PLUGIN_SERVER: Unsupported ctrl net msg from client\n");
	}

}

/*
 * Plugin server loop thread to poll on server socket for new connections
 * as well as new messages from existing connections
 *
 * @param: void *arg
 *
 * return: void *
 */
static void *octep_plugin_server_loop(void *arg)
{
	socklen_t peer_sz = sizeof(struct sockaddr_in);
	struct octep_plugin_msg msg = { 0 };
	struct sockaddr_in peer_addr;
	struct timeval tm = { 0 };
	char s[INET6_ADDRSTRLEN];
	int i, max_sockfd, client_sockfd, ret;
	void *in_addr;

	while (true) {
		/* Fdset needs to be reinitialised each time as select will
		 * manipulate fdset values
		 */
		FD_ZERO(&plugin_client_fdset);
		FD_SET(server_sockfd, &plugin_client_fdset);
		max_sockfd = server_sockfd;

		for (i = 0; i < OCTEP_PLUGIN_MAX_CLIENTS; i++) {
			client_sockfd = plugin_client[i].sockfd;

			if (client_sockfd != OCTEP_PLUGIN_INVALID_CLIENT_SOCKFD)
				FD_SET(client_sockfd, &plugin_client_fdset);
			else
				continue;

			if (client_sockfd > max_sockfd)
				max_sockfd = client_sockfd;
		}

		/* Poll on sockfds from server_sockfd to max_sockfd, and set plugin_client_fdset
		 * based on availability of new msgs or connections.
		 *
		 * New connections will appear on server_sockfd
		 * New msgs will appear on client sockfds
		 */
		ret = select(max_sockfd + 1, &plugin_client_fdset, NULL, NULL, &tm);
		if ((ret < 0) && (errno != EINTR)) {
			printf("PLUGIN_SERVER: Select return %d on error: %s",
			       ret, strerror(errno));
			return NULL;
		}

		if (FD_ISSET(server_sockfd, &plugin_client_fdset)) {
			client_sockfd = accept(server_sockfd,
					       (struct sockaddr *) &peer_addr, &peer_sz);
			if (client_sockfd < 0) {
				perror("PLUGIN_SERVER: accept:");
				continue;
			}

			in_addr = get_in_addr((struct sockaddr *)&peer_addr);
			inet_ntop(peer_addr.sin_family, in_addr, s, sizeof(s));
			for (i = 0; i < OCTEP_PLUGIN_MAX_CLIENTS; i++) {
				if (plugin_client[i].client_id == OCTEP_PLUGIN_INVALID_CLIENT_ID) {
					plugin_client[i].client_id = i;
					plugin_client[i].sockfd = client_sockfd;
					plugin_client[i].state =
					OCTEP_PLUGIN_CLIENT_STATE_CONNECTED;
					break;
				}
			}

			if (i == OCTEP_PLUGIN_MAX_CLIENTS) {
				printf("PLUGIN_SERVER: Unable to connect %s as number of clients saturated",
				       s);
				close(client_sockfd);
			}
			printf("PLUGIN_SERVER: New connection from client: %s\n", s);
		}

		for (i = 0; i < OCTEP_PLUGIN_MAX_CLIENTS; i++) {
			if (plugin_client[i].sockfd == OCTEP_PLUGIN_INVALID_CLIENT_SOCKFD)
				continue;

			client_sockfd = plugin_client[i].sockfd;
			if (FD_ISSET(client_sockfd, &plugin_client_fdset)) {
				ret = read(client_sockfd, &msg.hdr, sizeof(msg.hdr));
				if (ret == 0) {
					getpeername(client_sockfd, (struct sockaddr *)&peer_addr,
						    (socklen_t *) &peer_sz);
					in_addr = get_in_addr((struct sockaddr *)&peer_addr);
					inet_ntop(peer_addr.sin_family, in_addr, s, sizeof(s));
					printf("PLUGIN_SERVER: Client %s disconnected", s);
					close(client_sockfd);
					plugin_client[i].sockfd =
					OCTEP_PLUGIN_INVALID_CLIENT_SOCKFD;
					plugin_client[i].state = OCTEP_PLUGIN_CLIENT_STATE_INVAL;
					plugin_client[i].client_id = OCTEP_PLUGIN_INVALID_CLIENT_ID;
				} else {
					octep_plugin_client_msg_hdr_dump(&msg);
					ret = read(client_sockfd, &msg.data, msg.hdr.sz);
					if (ret < msg.hdr.sz) {
						printf("PLUGIN_SERVER: Incomplete msg received!\n");
						continue;
					}

					switch (msg.hdr.id) {
					case OCTEP_PLUGIN_C2S_MSG_CTRL_NET_NOTIFY:
					case OCTEP_PLUGIN_C2S_MSG_CTRL_NET_RESP:
						octep_plugin_client_msg_data_dump(&msg, true);
						plugin_fwd_to_host(&plugin_client[i], &msg);
						break;
					default:
						octep_plugin_client_msg_data_dump(&msg, false);
						plugin_handle_client_msg(&plugin_client[i], &msg);
						break;
					}
					memset(&msg, 0, sizeof(msg));
				}
			}
		}

	}

	return NULL;

}

/*
 * Find the plugin client app connection fd from client id given
 *
 * @param: int client_id
 * return: (int) connection fd of client on success,
 *         OCTEP_PLUGIN_INVALID_CLIENT_SOCKFD on failure
 */
static int find_plugin_client_connection(int client_id)
{
	if (client_id == OCTEP_PLUGIN_INVALID_CLIENT_ID)
		return OCTEP_PLUGIN_INVALID_CLIENT_SOCKFD;

	return plugin_client[client_id].sockfd;
}

/*
 * Initialise server socket and start octep_plugin_server_loop thread
 *
 * @param: struct plugin_app_cfg *app_cfg containing plugin information
 *	   about interfaces.
 *
 * return: (int) 0 on success, -errno on failure
 */
__attribute__((visibility("default")))
int octep_plugin_server_init(struct plugin_app_cfg *app_cfg)
{
	struct sockaddr_in server_addr = {
		.sin_family = AF_INET,
		.sin_port = htons(OCTEP_PLUGIN_SERVER_PORT),
		.sin_addr.s_addr = htonl(INADDR_LOOPBACK)
	};
	struct sockaddr_in sockaddr;
	int err, yes = 1;
	socklen_t len;

	if (!app_cfg) {
		printf("PLUGIN_SERVER: Init failed due to null cfg!");
		return -EINVAL;
	}

	server_sockfd = socket(AF_INET, SOCK_STREAM, 0);
	if (server_sockfd <= 0) {
		perror("PLUGIN_SERVER: Error in socket cmd");
		return -errno;
	}

	err = setsockopt(server_sockfd, SOL_SOCKET, SO_REUSEADDR, &yes, sizeof(int));
	if (err < 0) {
		printf("PLUGIN_SERVER: Error in setsockopt: %s\n", strerror(errno));
		close(server_sockfd);
		return -errno;
	}

	len = sizeof(struct sockaddr_in);
	err = bind(server_sockfd, (struct sockaddr *)&server_addr, len);
	if (err < 0) {
		printf("PLUGIN_SERVER: Error in bind: %s\n", strerror(errno));
		close(server_sockfd);
		return -errno;
	}

	err = getsockname(server_sockfd, (struct sockaddr *)&sockaddr, &len);
	if (err < 0) {
		printf("PLUGIN_SERVER: Error in getsockname: %s\n", strerror(errno));
		close(server_sockfd);
		return -errno;
	}

	err = listen(server_sockfd, 1);
	if (err < 0) {
		printf("PLUGIN_SERVER: Error in listen: %s\n", strerror(errno));
		close(server_sockfd);
		return -errno;
	}

	err = pthread_mutex_init(&plugin_server.ctrl_net_lock, NULL);
	if (err) {
		printf("PLUGIN_SERVER: Error on ctrl net lock init, err %d\n", err);
		return err;
	}

	err = pthread_create(&process_thread, NULL, octep_plugin_server_loop, NULL);
	if (err) {
		printf("PLUGIN_SERVER: Error while starting server thread: %s\n",
				strerror(errno));
		return err;
	}

	printf("Listening on %s:%d\n",
			inet_ntoa(sockaddr.sin_addr), sockaddr.sin_port);

	memcpy(&cfg, app_cfg, sizeof(*app_cfg));

	return 0;

}

/*
 * Lock mutex for control net access functions
 *
 * @param: void
 *
 * return: (int) 0 on success, -errno on failure
 */
__attribute__((visibility("default")))
int octep_plugin_server_ctrl_net_lock(void)
{
	return pthread_mutex_lock(&plugin_server.ctrl_net_lock);
}

/*
 * Host request handler for plugin server. Msg will be forwarded to client
 * if request is to valid plugin controlled interface and valid client.
 *
 * @param: struct octep_cp_msg *msg
 * return: (int) 0 on success, -errno on failure
 */
__attribute__((visibility("default")))
int octep_plugin_server_process_msg(struct octep_cp_msg *msg)
{
	struct octep_plugin_msg plugin_msg = { 0 };
	struct plugin_fn_cfg *fn;
	int sockfd;

	fn = plugin_app_config_get_fn(&cfg, &msg->info);
	if (!fn || !fn->plugin_controlled)
		return -EINVAL;

	sockfd = find_plugin_client_connection(fn->client_id);
	if (sockfd == OCTEP_PLUGIN_INVALID_CLIENT_SOCKFD) {
		printf("PLUGIN_SERVER: Request from unregistered client controlled interface "
		       "pem[%d]pf[%d]vf[%d] or interface points to stale client sockfd (client_id: %d)\n",
		       msg->info.s.pem_idx, msg->info.s.pf_idx,
		       msg->info.s.vf_idx, fn->client_id);
		return -EINVAL;
	}

	plugin_msg.hdr.id = OCTEP_PLUGIN_S2C_MSG_CTRL_NET;
	plugin_msg.hdr.sz = sizeof(struct octep_cp_msg);
	memcpy(&plugin_msg.data, msg, plugin_msg.hdr.sz);

	return plugin_fwd_to_app(sockfd, &plugin_msg);

}

/*
 * Event handler for plugin server. Forwards event info to connected client apps
 *
 * @param: struct octep_cp_event_info *event
 *
 * return: void
 */
__attribute__((visibility("default")))
void octep_plugin_server_process_event(struct octep_cp_event_info *event)
{
	/* int pem; */

	switch (event->e) {
	case OCTEP_CP_EVENT_TYPE_PERST:
		/* pem = event->u.perst.dom_idx; */
		/* TODO */
		break;
	case OCTEP_CP_EVENT_TYPE_FLR:
		/* pem = event->u.flr.dom_idx; */
		/* TODO */
		break;
	default:
		break;
	}

}

/*
 * Send host version of pem::pf to any related connected client.
 *
 * @param: uint16_t pem, uint16_t pf, uint32_t host_version
 *
 * return: void
 */
__attribute__((visibility("default")))
void octep_plugin_server_relay_host_version(uint16_t pem, uint16_t pf, uint32_t host_vers)
{
	octep_plugin_server_relay_force_host_version(pem, pf, host_vers, 0);
}

/*
 * Unlock mutex for control net access functions
 *
 * @param: void
 *
 * return: (int) 0 on success, -errno on failure
 */
__attribute__((visibility("default")))
int octep_plugin_server_ctrl_net_unlock(void)
{
	return pthread_mutex_unlock(&plugin_server.ctrl_net_lock);
}

/*
 * Uninitialises plugin server. Cancels octep_plugin_server_loop and destroys
 * control net access mutex lock.
 *
 * @param: void
 *
 * return: void
 */
__attribute__((visibility("default")))
void octep_plugin_server_uninit(void)
{
	pthread_cancel(process_thread);
	pthread_mutex_destroy(&plugin_server.ctrl_net_lock);
}

/*
 * Dump octep_plugin_msg_hdr for given octep_plugin_msg
 *
 * @param: struct octep_plugin_msg *msg
 *
 * return: void
 */
__attribute__((visibility("default")))
void octep_plugin_client_msg_hdr_dump(struct octep_plugin_msg *msg)
{
#if PLUGIN_CLIENT_MSG_DUMP
	printf("PLUGIN_SERVER: octep plugin msg hdr { id=%u dev_id.pem=%u dev_id.pf=%u "
	       "dev_id.vf=%u sz=%u }\n", msg->hdr.id, msg->hdr.dev_id.pem, msg->hdr.dev_id.pf,
	       msg->hdr.dev_id.vf, msg->hdr.sz);
#endif
}

/*
 * Dump data from given octep_plugin_msg.
 *
 * @param: struct octep_plugin_msg *msg, bool is_cp_msg
 *
 * return: void
 */
__attribute__((visibility("default")))
void octep_plugin_client_msg_data_dump(struct octep_plugin_msg *msg, bool is_cp_msg)
{
#if PLUGIN_CLIENT_MSG_DUMP
	struct octep_cp_msg *cp_msg;
	int i;

	printf("PLUGIN_SERVER: octep plugin msg data {");

	if (!is_cp_msg) {
		for (i = 0; i < msg->hdr.sz; i++)
			printf("0x%x ", msg->data[i]);
	} else {
		cp_msg = (struct octep_cp_msg *) &msg->data;

		printf("\n\toctep_cp_msg { .info={ 0x%lx 0x%lx } .sg_num=%d\n",
		       cp_msg->info.words[0], cp_msg->info.words[1],
		       cp_msg->sg_num);
		for (i = 0; i < cp_msg->sg_num; i++) {
			printf("\t.sg_list[%d]={\n\t\t.sz=%u .msg=%p",
			       i, cp_msg->sg_list[i].sz, cp_msg->sg_list[i].msg);
			printf("\n\t\t}\n");
		}
		printf("\t}\n");
	}

	printf("}\n");
#endif
}

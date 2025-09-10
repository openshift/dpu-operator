// SPDX-License-Identifier: BSD-3-Clause
/* Copyright (c) 2022 Marvell.
 */
#include <stdio.h>
#include <stdlib.h>
#include <stdbool.h>
#include <unistd.h>
#include <errno.h>
#include <arpa/inet.h>
#include <pthread.h>
#include <string.h>
#include <fcntl.h>
#include <sys/time.h>

#include "octep_cp_lib.h"
#include "octep_ctrl_net.h"
#include "octep_plugin_client.h"

#define PLUGIN_VERSION_MAJOR		1
#define PLUGIN_VERSION_MINOR		1
#define PLUGIN_VERSION_VARIANT		0

#define OCTEP_PLUGIN_CLIENT_VERSION	(OCTEP_PLUGIN_VERSION(PLUGIN_VERSION_MAJOR, \
							      PLUGIN_VERSION_MINOR, \
							      PLUGIN_VERSION_VARIANT))

static struct octep_plugin_client_info plugin_client = {
	.state = OCTEP_PLUGIN_CLIENT_STATE_INVALID,
	.client_sockfd = 0,
	.num_devs = 0,
	.info = NULL,
	.dev_list = { [0 ... OCTEP_PLUGIN_CLIENT_MAX_DEVICES - 1].pem = OCTEP_PLUGIN_MAX_PEM,
		      [0 ... OCTEP_PLUGIN_CLIENT_MAX_DEVICES - 1].pf = OCTEP_PLUGIN_MAX_PF_PER_PEM,
		      [0 ... OCTEP_PLUGIN_CLIENT_MAX_DEVICES - 1].vf = OCTEP_PLUGIN_MAX_VF_PER_PF,
	}
};

uint32_t octep_plugin_client_host_version[OCTEP_PLUGIN_MAX_PEM]
					 [OCTEP_PLUGIN_MAX_PF_PER_PEM] = { 0 };

static inline void *get_in_addr(struct sockaddr *sa)
{
	return (sa->sa_family == AF_INET) ?
	       (void *)(&(((struct sockaddr_in *) sa)->sin_addr)) :
	       (void *)(&(((struct sockaddr_in6 *) sa)->sin6_addr));
}

int octep_plugin_client_init(struct octep_plugin_info *info)
{
	int ret;

	ret = socket(AF_INET, SOCK_STREAM, 0);
	if (ret <= 0) {
		perror("PLUGIN_CLIENT: Socket error:");
		return -errno;
	}
	plugin_client.client_sockfd = ret;
	plugin_client.info = info;
	plugin_client.state = OCTEP_PLUGIN_CLIENT_STATE_INIT;

	return 0;
}

static int octep_plugin_client_host_version_update(struct octep_plugin_msg *msg)
{
	struct octep_plugin_dev_id *dev_id = &msg->hdr.dev_id;

	octep_plugin_client_host_version[dev_id->pem][dev_id->pf] = *(uint32_t *) &msg->data;
	return 0;
}

static int octep_plugin_client_send_msg(int cmd, int data, struct octep_plugin_dev_id *id)
{
	struct octep_plugin_msg msg = { 0 }, reply = { 0 };
	struct timeval tv_b = { 0 }, tv_a = { 0 }, tv_ref;
	int msg_sz, ret;

	if (id)
		memcpy(&msg.hdr.dev_id, id, sizeof(*id));

	msg.hdr.id = cmd;
	msg.hdr.sz = sizeof(int);
	*(int *)&msg.data = data;
	msg_sz = sizeof(msg.hdr) + msg.hdr.sz;

	tv_ref.tv_sec = 5;
	ret = send(plugin_client.client_sockfd, &msg, msg_sz, 0);
	if (ret != msg_sz) {
		printf("PLUGIN_CLIENT: Cmd to server send unsuccessful!\n");
		return -EIO;
	}

	gettimeofday(&tv_b, NULL);
	while (true) {
		gettimeofday(&tv_a, NULL);
		if ((tv_a.tv_sec - tv_b.tv_sec) >= tv_ref.tv_sec) {
			printf("PLUGIN_CLIENT: Cmd to server %d timed out after %ld\n",
			       cmd, tv_a.tv_sec);
			return -EIO;
		}

		if (cmd == OCTEP_PLUGIN_C2S_MSG_DEV_UNREGISTER)
			break;

		ret = read(plugin_client.client_sockfd, &reply.hdr, sizeof(reply.hdr));
		if (ret == 0) {
			printf("PLUGIN_CLIENT: Server connection closed unexpectedly\n");
			return -EIO;
		}

		ret = read(plugin_client.client_sockfd, &reply.data, reply.hdr.sz);
		if (ret != reply.hdr.sz) {
			printf("PLUGIN_CLIENT: Unexpected response size %d of %d expected\n",
					ret, reply.hdr.sz);
			return -EIO;
		}

		if (reply.hdr.id == OCTEP_PLUGIN_S2C_MSG_PLUGIN_RESP)
			break;

		printf("PLUGIN_CLIENT: Cmd to server failed, obtained invalid response from server\n");
		return -EIO;
	}

	return 0;
}

static int octep_plugin_client_host_version_get(void)
{
	struct timeval tv_b = { 0 }, tv_a = { 0 }, tv_c = { 0 }, tv_ref;
	struct octep_plugin_msg reply = { 0 };
	int ret;

	tv_ref.tv_sec = 5;
	gettimeofday(&tv_b, NULL);
	while (true) {
		gettimeofday(&tv_a, NULL);
		if ((tv_a.tv_sec - tv_b.tv_sec) >= tv_ref.tv_sec) {
			printf("PLUGIN_CLIENT: Host version get timed out after %ld\n",
			       tv_a.tv_sec);
			return -EIO;
		}

		ret = read(plugin_client.client_sockfd, &reply.hdr, sizeof(reply.hdr));
		if (ret == 0) {
			printf("PLUGIN_CLIENT: Server connection closed unexpectedly\n");
			return -EIO;
		}

		ret = read(plugin_client.client_sockfd, &reply.data, reply.hdr.sz);
		if (ret != reply.hdr.sz) {
			printf("PLUGIN_CLIENT: Unexpected response size %d of %d expected\n",
					ret, reply.hdr.sz);
			return -EIO;
		}

		if (reply.hdr.id == OCTEP_PLUGIN_S2C_MSG_PLUGIN_RESP)
			break;

		if (reply.hdr.id != OCTEP_PLUGIN_S2C_MSG_HOST_VERSION) {
			printf("PLUGIN_CLIENT: Obtained invalid message from server %d."
			       "Expecting host version\n",
			       reply.hdr.id);
			return -EIO;
		}

		octep_plugin_client_host_version_update(&reply);
		gettimeofday(&tv_c, NULL);
		/* Not account time used for valid loop */
		tv_b.tv_sec += (tv_c.tv_sec - tv_a.tv_sec);
	}

	return 0;
}

int octep_plugin_client_start(void)
{
	struct sockaddr_in server_addr = {
		.sin_family = AF_INET,
		.sin_port = htons(OCTEP_PLUGIN_SERVER_PORT),
		.sin_addr.s_addr = htonl(INADDR_LOOPBACK)
	};
	int ret;

	if (plugin_client.state != OCTEP_PLUGIN_CLIENT_STATE_INIT) {
		printf("PLUGIN_CLIENT: Cannot start already started or uninitialised client\n");
		return -EINVAL;
	}

	ret = connect(plugin_client.client_sockfd, (struct sockaddr *) &server_addr,
		      sizeof(server_addr));
	if (ret) {
		perror("PLUGIN_CLIENT: Connect error:");
		goto error;
	}

	plugin_client.state = OCTEP_PLUGIN_CLIENT_STATE_CONNECTED;
	printf("PLUGIN_CLIENT: Connected to PLUGIN SERVER successfully at port %d\n",
		OCTEP_PLUGIN_SERVER_PORT);

	ret = octep_plugin_client_send_msg(OCTEP_PLUGIN_C2S_MSG_INIT, OCTEP_PLUGIN_CLIENT_VERSION,
				       NULL);
	if (ret < 0) {
		printf("PLUGIN_CLIENT: Plugin client start failed\n");
		errno = -ret;
		goto error;
	}

	printf("PLUGIN_CLIENT: Successfully exchanged version between plugin client and server: v%d\n",
	       OCTEP_PLUGIN_CLIENT_VERSION);

	ret = octep_plugin_client_host_version_get();
	if (ret < 0) {
		printf("PLUGIN_CLIENT: Host version get failed with err %d. Uninitialising...\n",
		       ret);
		goto error;
	}

	plugin_client.state = OCTEP_PLUGIN_CLIENT_STATE_CONNECTED;

	return 0;

error:
	close(plugin_client.client_sockfd);
	plugin_client.client_sockfd = 0;
	plugin_client.info = NULL;
	plugin_client.state = OCTEP_PLUGIN_CLIENT_STATE_INVALID;
	return -errno;
}

int octep_plugin_client_dev_register(struct octep_plugin_dev_id *id)
{
	int i, ret;

	if (plugin_client.state != OCTEP_PLUGIN_CLIENT_STATE_CONNECTED) {
		printf("PLUGIN_CLIENT: Client not connected to server yet to register\n");
		return -EINVAL;
	}

	if (!id || id->pem == OCTEP_PLUGIN_MAX_PEM || id->pf == OCTEP_PLUGIN_MAX_PF_PER_PEM) {
		printf("PLUGIN_CLIENT: Invalid device id\n");
		return -EINVAL;
	}

	for (i = 0; i < plugin_client.num_devs; i++) {
		if (plugin_client.dev_list[i].pem == id->pem &&
		    plugin_client.dev_list[i].pf == id->pf &&
		    plugin_client.dev_list[i].vf == id->vf) {
			printf("PLUGIN_CLIENT: Device pem%d:pf%d",
			       id->pem, id->pf);
			if (id->vf != OCTEP_PLUGIN_INVALID_VF_IDX)
				printf(":vf%d ", id->vf);
			printf("already registered\n");
			return -EINVAL;
		}
	}

	if (plugin_client.num_devs == OCTEP_PLUGIN_CLIENT_MAX_DEVICES) {
		printf("PLUGIN_CLIENT: Device registration failed, no more free entries\n");
		return -ENOMEM;
	}

	ret = octep_plugin_client_send_msg(OCTEP_PLUGIN_C2S_MSG_DEV_REGISTER, 0, id);
	if (ret < 0) {
		printf("PLUGIN_CLIENT: Device register send cmd failed\n");
		return ret;
	}

	memcpy(&plugin_client.dev_list[plugin_client.num_devs], id, sizeof(*id));
	plugin_client.num_devs++;
	printf("PLUGIN_CLIENT: Device pem%d::pf%d", id->pem, id->pf);
	if (id->vf != OCTEP_PLUGIN_INVALID_VF_IDX)
		printf("::vf%d ", id->vf);
	printf("registered successfully\n");

	return 0;
}

int octep_plugin_client_dev_unregister(struct octep_plugin_dev_id *id)
{
	int i, ret, err = 0, num_devs = plugin_client.num_devs;

	if (plugin_client.state != OCTEP_PLUGIN_CLIENT_STATE_CONNECTED) {
		printf("PLUGIN_CLIENT: Client not connected to server yet to unregister anything\n");
		return -EINVAL;
	}

	if (id) {
		ret = octep_plugin_client_send_msg(OCTEP_PLUGIN_C2S_MSG_DEV_UNREGISTER, 0, id);
		if (ret < 0)
			printf("PLUGIN_CLIENT: Device unregistration send cmd failed\n");
		else
			plugin_client.num_devs--;

		return ret;
	}

	for (i = 0; i < num_devs; i++) {
		ret = octep_plugin_client_send_msg(OCTEP_PLUGIN_C2S_MSG_DEV_UNREGISTER, 0,
					       &plugin_client.dev_list[i]);
		if (ret < 0) {
			err = ret;
			printf("PLUGIN_CLIENT: Device unregistration send cmd failed\n");
			continue;
		}
		plugin_client.num_devs--;
	}

	return err;
}

int octep_plugin_client_stop(void)
{
	int ret, err = 0;

	switch (plugin_client.state) {
	case OCTEP_PLUGIN_CLIENT_STATE_CONNECTED:
		ret = octep_plugin_client_dev_unregister(NULL);
		if (ret)
			err = ret;
		/* Fallthrough */
	case OCTEP_PLUGIN_CLIENT_STATE_INIT:
		close(plugin_client.client_sockfd);
		plugin_client.state = OCTEP_PLUGIN_CLIENT_STATE_INVALID;
		/* Fallthrough */
	case OCTEP_PLUGIN_CLIENT_STATE_INVALID:
		/* Fallthrough */
	default:
		break;
	}

	if (err)
		printf("PLUGIN_CLIENT: Client stop failed\n");

	return err;
}

int octep_plugin_client_poll(struct octep_plugin_msg *msg)
{
	struct octep_cp_msg *cp_msg;
	int ret, i, flags;
	uint8_t *buf;

	if (plugin_client.state != OCTEP_PLUGIN_CLIENT_STATE_CONNECTED) {
		printf("PLUGIN_CLIENT: Poll error, client is not in connected state\n");
		return -EINVAL;
	}

	if (!msg) {
		printf("PLUGIN_CLIENT: Null msg pointer provided\n");
		return -EINVAL;
	}


	flags = fcntl(plugin_client.client_sockfd, F_GETFL, 0);
	if (flags < 0) {
		perror("PLUGIN_CLIENT: Unable to get socket flags:");
		return -EIO;
	}

	if (!(flags & O_NONBLOCK)) {
		flags |= O_NONBLOCK;
		ret = fcntl(plugin_client.client_sockfd, F_SETFL, flags);
		if (ret < 0) {
			perror("PLUGIN_CLIENT: Unable to set socket as non-blocking:");
			return -EIO;
		}
	}

	ret = read(plugin_client.client_sockfd, &msg->hdr,
		   sizeof(msg->hdr));
	if (ret == 0) {
		printf("PLUGIN_CLIENT: Server connection closed unexpectedly\n");
		return -EIO;
	} else if (ret > 0) {
		if (msg->hdr.id != OCTEP_PLUGIN_S2C_MSG_CTRL_NET &&
		    msg->hdr.id != OCTEP_PLUGIN_S2C_MSG_HOST_VERSION) {
			printf("PLUGIN_CLIENT: Unexpected msg id %d\n", msg->hdr.id);
			return -EIO;
		}

		ret = read(plugin_client.client_sockfd, &msg->data,
			   msg->hdr.sz);
		if (ret != msg->hdr.sz) {
			printf("PLUGIN_CLIENT: Unexpected msg size read, %d of %d specified\n",
			       ret, msg->hdr.sz);
			return -EIO;
		}

		if (msg->hdr.id == OCTEP_PLUGIN_S2C_MSG_HOST_VERSION)
			return octep_plugin_client_host_version_update(msg);

		cp_msg = (struct octep_cp_msg *) &msg->data;
		buf = &msg->data[msg->hdr.sz];
		for (i = 0; i < cp_msg->sg_num; i++) {
			ret = read(plugin_client.client_sockfd, buf,
			     cp_msg->sg_list[i].sz);
			if (ret != cp_msg->sg_list[i].sz) {
				printf("PLUGIN_CLIENT: Unexpected sg_list[%d] size read, %d of %d specified\n",
				       i, ret, cp_msg->sg_list[i].sz);
				return -EIO;
			}
			cp_msg->sg_list[i].msg = buf;
			buf += cp_msg->sg_list[i].sz;
		}

		return msg->hdr.sz;
	} else if ((errno != EAGAIN) || (errno != EWOULDBLOCK)) {
		printf("PLUGIN_CLIENT: Read error on socket\n");
		return -errno;
	}

	return 0;
}

int octep_plugin_client_send_notification(struct octep_plugin_msg *msg)
{
	int i, j, msg_sz, ret, sg_sz = 0;
	struct octep_cp_msg *cp_msg;
	uint8_t *buf;

	if (plugin_client.state != OCTEP_PLUGIN_CLIENT_STATE_CONNECTED) {
		printf("PLUGIN_CLIENT: Send notif error, client is not in connected state\n");
		return -EINVAL;
	}

	if (!msg) {
		printf("PLUGIN_CLIENT: Invalid plugin msg\n");
		return -EINVAL;
	}

	for (i = 0; i < plugin_client.num_devs; i++) {
		if (plugin_client.dev_list[i].pem == msg->hdr.dev_id.pem &&
		    plugin_client.dev_list[i].pf == msg->hdr.dev_id.pf &&
		    plugin_client.dev_list[i].vf == msg->hdr.dev_id.vf) {

			cp_msg = (struct octep_cp_msg *) &msg->data;
			buf = &msg->data[msg->hdr.sz];
			for (j = 0; j < cp_msg->sg_num; j++) {
				memcpy(buf, cp_msg->sg_list[j].msg, cp_msg->sg_list[j].sz);
				buf += cp_msg->sg_list[j].sz;
				sg_sz += cp_msg->sg_list[j].sz;
			}
			msg_sz = sizeof(msg->hdr) + msg->hdr.sz + sg_sz;

			ret = send(plugin_client.client_sockfd, msg,
				   msg_sz, 0);
			if (ret != msg_sz) {
				printf("PLUGIN_CLIENT: Send notif/response failed, socket send unsuccessful\n");
				return -EIO;
			}
			printf("PLUGIN_CLIENT: Notif/Response sent successfully\n");
			return 0;
		}
	}

	printf("PLUGIN_CLIENT: Send notif failed, target dev does not belong to app\n");
	return -EINVAL;
}

int octep_plugin_client_get_state(uint32_t *state)
{
	if (!state)
		return -EINVAL;

	*state = plugin_client.state;
	return 0;
}

int octep_plugin_client_uninit(void)
{
	if (plugin_client.state != OCTEP_PLUGIN_CLIENT_STATE_INVALID) {
		printf("PLUGIN_CLIENT: Error, client needs to stop before uninitialising\n");
		return -EINVAL;
	}

	if (plugin_client.info)
		memset(plugin_client.info, 0, sizeof(*plugin_client.info));
	plugin_client.info = NULL;

	return 0;
}

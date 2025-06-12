// SPDX-License-Identifier: BSD-3-Clause
/* Copyright (c) 2022 Marvell.
 */
#include <signal.h>
#include <stdio.h>
#include <stdint.h>
#include <unistd.h>
#include <errno.h>
#include <string.h>
#include <sys/time.h>
#include <time.h>
#include <getopt.h>
#include <stdlib.h>

#include "octep_cp_lib.h"
#include "loop.h"
#include "app_config.h"

/* Control plane version */
#define CP_VERSION_MAJOR		1
#define CP_VERSION_MINOR		0
#define CP_VERSION_VARIANT		0

#define CP_VERSION_CURRENT		(OCTEP_CP_VERSION(CP_VERSION_MAJOR, \
							  CP_VERSION_MINOR, \
							  CP_VERSION_VARIANT))

#define MAX_NUM_MSG			6

static volatile int force_quit = 0;
static volatile int perst[APP_CFG_PEM_MAX] = { 0 };
static int hb_interval = 0;
struct octep_cp_lib_cfg cp_lib_cfg = { 0 };

static timer_t tim;
static struct itimerspec itim = { 0 };
static struct timespec cpu_yield_tspec = {
	.tv_sec = 0,
	.tv_nsec = 1 * 1000000
};
static int max_num_msg = MAX_NUM_MSG;
static struct octep_cp_event_info *ev;

static int app_handle_perst(int dom_idx);

static int process_events()
{
	int n, i, err;

	n = octep_cp_lib_recv_event(ev, max_num_msg);
	if (n < 0)
		return n;

	for (i = 0; i < n; i++) {
		if (ev[i].e == OCTEP_CP_EVENT_TYPE_PERST) {
			printf("APP: Event: perst on dom[%d]\n",
			       ev[i].u.perst.dom_idx);
			err = app_handle_perst(ev[i].u.perst.dom_idx);
			if (err) {
				printf("APP: Unable to handle perst event on PEM %d!\n",
				       ev[i].u.perst.dom_idx);
				return err;
			}
		}
	}

	return 0;
}

static int send_heartbeat()
{
	struct octep_cp_event_info info;
	int i, j;

	info.e = OCTEP_CP_EVENT_TYPE_HEARTBEAT;
	for (i=0; i<cp_lib_cfg.ndoms; i++) {
		if (perst[i])
			continue;

		info.u.hbeat.dom_idx = cp_lib_cfg.doms[i].idx;
		for (j=0; j<cp_lib_cfg.doms[i].npfs; j++) {
			info.u.hbeat.pf_idx = cp_lib_cfg.doms[i].pfs[j].idx;
			octep_cp_lib_send_event(&info);
		}
	}

	return 0;
}

static void trigger_alarm(int hb_interval)
{
	itim.it_value.tv_sec = (hb_interval / 1000);
	itim.it_value.tv_nsec = (hb_interval % 1000) * 1000000;

	timer_settime(tim, 0, &itim, NULL);
}

void sigint_handler(int sig_num) {

	if (sig_num == SIGINT) {
		printf("APP: Program quitting.\n");
		force_quit = 1;
	} else if (sig_num == SIGALRM) {
		if (force_quit)
			return;

		send_heartbeat();
		trigger_alarm(hb_interval);
	}
}

static int set_fw_ready_for_pem(int dom_idx, int ready)
{
	struct octep_cp_event_info info;
	int j;

	info.e = OCTEP_CP_EVENT_TYPE_FW_READY;
	info.u.fw_ready.ready = ready;
	info.u.fw_ready.dom_idx = cp_lib_cfg.doms[dom_idx].idx;
	for (j = 0; j < cp_lib_cfg.doms[dom_idx].npfs; j++) {
		info.u.fw_ready.pf_idx = cp_lib_cfg.doms[dom_idx].pfs[j].idx;
		octep_cp_lib_send_event(&info);
	}

	return 0;
}

static int set_fw_ready(int ready)
{
	int i;

	for (i=0; i<cp_lib_cfg.ndoms; i++) {
		set_fw_ready_for_pem(i, ready);
	}

	return 0;
}

static int app_handle_perst(int dom_idx)
{
	struct pem_cfg *pem;
	int err;

	pem = &cfg.pems[dom_idx];
	if (!pem->valid)
		return -EINVAL;

	perst[dom_idx] = 1;
	set_fw_ready_for_pem(dom_idx, 0);
	octep_cp_lib_uninit_pem(dom_idx);
	loop_uninit_pem(dom_idx);
	printf("APP: Reinitiazing PEM %d\n", dom_idx);

	err = octep_cp_lib_init_pem(&cp_lib_cfg, dom_idx);
	if (err)
		return err;
	app_config_update_pem(dom_idx);
	err = loop_init_pem(dom_idx);
	if (err) {
		octep_cp_lib_uninit_pem(dom_idx);
		return err;
	}
	app_config_print_pem(dom_idx);
	set_fw_ready_for_pem(dom_idx, 1);
	perst[dom_idx] = 0;
	return 0;
}

/* display usage */
static void print_usage(const char *prgname)
{
	printf("%s config_file\n"
	       "  -y <milliseconds>\n"
	       "    yield cpu for msecs between subsequent calls to msg poll (default: 1ms)\n"
	       "  -m <1-n>\n"
	       "    Max control messages and events to be polled at one time (default: 6)\n",
	       prgname);
}

static const char short_options[] =
	"y:"  /* cpu yield */
	"m:"  /* max msg count */
	;

static const struct option lgopts[] = {
	{NULL, 0, 0, 0}
};

int parse_args(int argc, char **argv)
{
	int opt, cpu_yield_ms;
	char **argvopt;
	int option_index;
	char *prgname = argv[0];

	argvopt = argv;

	while ((opt = getopt_long(argc, argvopt, short_options,
				  lgopts, &option_index)) != EOF) {
		switch (opt) {
		case 'y':
			cpu_yield_ms = atoi(optarg);
			if (cpu_yield_ms <= 0)
				cpu_yield_ms = 1;

			cpu_yield_tspec.tv_sec = (cpu_yield_ms / 1000);
			cpu_yield_tspec.tv_nsec = ((cpu_yield_ms % 1000) * 1000000);

			break;
		case 'm':
			max_num_msg = atoi(optarg);
			if (max_num_msg <= 0)
				max_num_msg = 6;

			break;
		default:
			print_usage(prgname);
			return -1;
		}
	}

	if (optind >= 0)
		argv[optind-1] = prgname;

	return optind - 1;
}

int main(int argc, char *argv[])
{
	int err = 0, src_i, src_j, dst_i, dst_j;
	struct pem_cfg *pem;
	struct pf_cfg *pf;

	if (argc < 2) {
		print_usage(argv[0]);
		return -EINVAL;
	}

	err = app_config_init(argv[1]);
	if (err)
		return err;

	/* skip program name and config file params */
	optind = 2;
	parse_args(argc, argv);

	ev = calloc(max_num_msg, sizeof(struct octep_cp_event_info));
	if (!ev)
		return -ENOMEM;

	signal(SIGINT, sigint_handler);
	signal(SIGALRM, sigint_handler);

	timer_create(CLOCK_REALTIME, NULL, &tim);

	printf("APP: cpu yield time (-y) = %lds %ldns\n", cpu_yield_tspec.tv_sec,
							  cpu_yield_tspec.tv_nsec);
	printf("APP: max control msgs/events per poll (-m) = %d\n", max_num_msg);

	hb_interval = 0;
	cp_lib_cfg.min_version = CP_VERSION_CURRENT;
	cp_lib_cfg.max_version = CP_VERSION_CURRENT;
	cp_lib_cfg.ndoms = cfg.npem;
	dst_i = 0;
	for (src_i = 0; src_i < APP_CFG_PEM_MAX; src_i++) {
		pem = &cfg.pems[src_i];
		if (!pem->valid)
			continue;

		cp_lib_cfg.doms[dst_i].idx = src_i;
		cp_lib_cfg.doms[dst_i].npfs = pem->npf;
		dst_j = 0;
		for (src_j = 0; src_j < APP_CFG_PF_PER_PEM_MAX; src_j++) {
			pf = &pem->pfs[src_j];
			if (!pf->valid)
				continue;

			cp_lib_cfg.doms[dst_i].pfs[dst_j].idx = src_j;
			if (hb_interval == 0 ||
			    pf->fn.info.hb_interval < hb_interval)
				hb_interval = pf->fn.info.hb_interval;

			dst_j++;
		}
		dst_i++;
	}
	err = octep_cp_lib_parse_args(argc, argv, &cp_lib_cfg);
	if (err)
		return err;

	err = octep_cp_lib_init(&cp_lib_cfg);
	if (err)
		return err;

	app_config_update();
	err = loop_init(max_num_msg);
	if (err) {
		octep_cp_lib_uninit();
		return err;
	}

	app_config_print();
	printf("APP: Heartbeat interval : %u msecs\n", hb_interval);

	set_fw_ready(1);
	trigger_alarm(hb_interval);
	while (!force_quit) {
		loop_process_msgs();
		process_events();
		nanosleep(&cpu_yield_tspec, NULL);
	}
	set_fw_ready(0);

	octep_cp_lib_uninit();
	loop_uninit();

	timer_delete(tim);
	app_config_uninit();

	return 0;
}

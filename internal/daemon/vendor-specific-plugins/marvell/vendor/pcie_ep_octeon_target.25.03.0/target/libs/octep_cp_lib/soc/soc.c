// SPDX-License-Identifier: BSD-3-Clause
/* Copyright (c) 2022 Marvell.
 */

#include <errno.h>
#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include <fcntl.h>
#include <unistd.h>
#include <dirent.h>
#include <stdbool.h>
#include <stdlib.h>

#include "octep_cp_lib.h"
#include "cp_log.h"
#include "cp_lib.h"
#include "octep_ctrl_mbox.h"
#include "cnxk.h"

#ifndef BIT_ULL
#define BIT_ULL(nr) (1ULL << (nr))
#endif

/* RoC and CPU IDs and revisions */
#define VENDOR_ARM    0x41 /* 'A' */
#define VENDOR_CAVIUM 0x43 /* 'C' */

#define SOC_PART_CN10K 0xD49

#define PART_106xx  0xB9
#define PART_105xx  0xBA
#define PART_105xxN 0xBC
#define PART_103xx  0xBD
#define PART_98xx   0xB1
#define PART_96xx   0xB2
#define PART_95xx   0xB3
#define PART_95xxN  0xB4
#define PART_95xxMM 0xB5
#define PART_95O    0xB6

#define MODEL_IMPL_BITS	  8
#define MODEL_IMPL_SHIFT  24
#define MODEL_IMPL_MASK	  ((1 << MODEL_IMPL_BITS) - 1)
#define MODEL_PART_BITS	  12
#define MODEL_PART_SHIFT  4
#define MODEL_PART_MASK	  ((1 << MODEL_PART_BITS) - 1)
#define MODEL_MAJOR_BITS  4
#define MODEL_MAJOR_SHIFT 20
#define MODEL_MAJOR_MASK  ((1 << MODEL_MAJOR_BITS) - 1)
#define MODEL_MINOR_BITS  4
#define MODEL_MINOR_SHIFT 0
#define MODEL_MINOR_MASK  ((1 << MODEL_MINOR_BITS) - 1)

#define MODEL_CN10K_PART_SHIFT	8
#define MODEL_CN10K_PASS_BITS	4
#define MODEL_CN10K_PASS_MASK	((1 << MODEL_CN10K_PASS_BITS) - 1)
#define MODEL_CN10K_MAJOR_BITS	2
#define MODEL_CN10K_MAJOR_SHIFT 2
#define MODEL_CN10K_MAJOR_MASK	((1 << MODEL_CN10K_MAJOR_BITS) - 1)
#define MODEL_CN10K_MINOR_BITS	2
#define MODEL_CN10K_MINOR_SHIFT 0
#define MODEL_CN10K_MINOR_MASK	((1 << MODEL_CN10K_MINOR_BITS) - 1)

#define	ARR_DIM(a)	(sizeof (a) / sizeof ((a)[0]))

static const struct model_db {
	uint32_t impl;
	uint32_t part;
	uint32_t major;
	uint32_t minor;
	uint64_t flag;
	char name[OCTEP_CP_SOC_MODEL_STR_LEN_MAX];
} model_db[] = {
	{VENDOR_ARM, PART_106xx, 0, 0, OCTEP_CP_SOC_MODEL_CN106xx_A0, "cn10ka_a0"},
	{VENDOR_ARM, PART_106xx, 0, 1, OCTEP_CP_SOC_MODEL_CN106xx_A1, "cn10ka_a1"},
	{VENDOR_ARM, PART_106xx, 1, 0, OCTEP_CP_SOC_MODEL_CN106xx_B0, "cn10ka_b0"},
	{VENDOR_ARM, PART_105xx, 0, 0, OCTEP_CP_SOC_MODEL_CNF105xx_A0, "cnf10ka_a0"},
	{VENDOR_ARM, PART_105xx, 0, 1, OCTEP_CP_SOC_MODEL_CNF105xx_A1, "cnf10ka_a1"},
	{VENDOR_ARM, PART_103xx, 0, 0, OCTEP_CP_SOC_MODEL_CN103xx_A0, "cn10kb_a0"},
	{VENDOR_ARM, PART_105xxN, 0, 0, OCTEP_CP_SOC_MODEL_CNF105xxN_A0, "cnf10kb_a0"},
	{VENDOR_ARM, PART_105xxN, 1, 0, OCTEP_CP_SOC_MODEL_CNF105xxN_B0, "cnf10kb_b0"},
	{VENDOR_CAVIUM, PART_98xx, 0, 0, OCTEP_CP_SOC_MODEL_CN98xx_A0, "cn98xx_a0"},
	{VENDOR_CAVIUM, PART_98xx, 0, 1, OCTEP_CP_SOC_MODEL_CN98xx_A1, "cn98xx_a1"},
	{VENDOR_CAVIUM, PART_96xx, 0, 0, OCTEP_CP_SOC_MODEL_CN96xx_A0, "cn96xx_a0"},
	{VENDOR_CAVIUM, PART_96xx, 0, 1, OCTEP_CP_SOC_MODEL_CN96xx_B0, "cn96xx_b0"},
	{VENDOR_CAVIUM, PART_96xx, 2, 0, OCTEP_CP_SOC_MODEL_CN96xx_C0, "cn96xx_c0"},
	{VENDOR_CAVIUM, PART_96xx, 2, 1, OCTEP_CP_SOC_MODEL_CN96xx_C0, "cn96xx_c1"},
	{VENDOR_CAVIUM, PART_95xx, 0, 0, OCTEP_CP_SOC_MODEL_CNF95xx_A0, "cnf95xx_a0"},
	{VENDOR_CAVIUM, PART_95xx, 1, 0, OCTEP_CP_SOC_MODEL_CNF95xx_B0, "cnf95xx_b0"},
	{VENDOR_CAVIUM, PART_95xxN, 0, 0, OCTEP_CP_SOC_MODEL_CNF95xxN_A0, "cnf95xxn_a0"},
	{VENDOR_CAVIUM, PART_95xxN, 0, 1, OCTEP_CP_SOC_MODEL_CNF95xxN_A0, "cnf95xxn_a1"},
	{VENDOR_CAVIUM, PART_95xxN, 1, 0, OCTEP_CP_SOC_MODEL_CNF95xxN_B0, "cnf95xxn_b0"},
	{VENDOR_CAVIUM, PART_95O, 0, 0, OCTEP_CP_SOC_MODEL_CNF95xxO_A0, "cnf95O_a0"},
	{VENDOR_CAVIUM, PART_95xxMM, 0, 0, OCTEP_CP_SOC_MODEL_CNF95xxMM_A0,
	 "cnf95xxmm_a0"}};

static struct octep_cp_soc_model model;

#define PCI_VENDOR_ID_CAVIUM		0x177D
#define PCI_DEVID_CNXK_RVU_PF		0xA063
#define PCI_DEVID_CNXK_RVU_VF		0xA064
#define PCI_DEVID_CNXK_RVU_AF		0xA065
#define PCI_DEVID_CN10K_RVU_CPT_PF	0xA0F2
#define PCI_DEVID_CN10K_RVU_CPT_VF	0xA0F3
#define PCI_DEVID_CNXK_RVU_AF_VF	0xA0f8
#define PCI_DEVID_CNXK_RVU_SSO_TIM_PF	0xA0F9
#define PCI_DEVID_CNXK_RVU_SSO_TIM_VF	0xA0FA
#define PCI_DEVID_CNXK_RVU_NPA_PF	0xA0FB
#define PCI_DEVID_CNXK_RVU_NPA_VF	0xA0FC

/* Detected SOC */
enum cp_lib_soc soc;

/* Detect if RVU device */
static bool is_rvu_device(unsigned long val)
{
	return (val == PCI_DEVID_CNXK_RVU_PF || val == PCI_DEVID_CNXK_RVU_VF ||
		val == PCI_DEVID_CNXK_RVU_AF ||
		val == PCI_DEVID_CNXK_RVU_AF_VF ||
		val == PCI_DEVID_CNXK_RVU_NPA_PF ||
		val == PCI_DEVID_CNXK_RVU_NPA_VF ||
		val == PCI_DEVID_CNXK_RVU_SSO_TIM_PF ||
		val == PCI_DEVID_CNXK_RVU_SSO_TIM_VF ||
		val == PCI_DEVID_CN10K_RVU_CPT_PF ||
		val == PCI_DEVID_CN10K_RVU_CPT_VF);
}

/* parse a sysfs (or other) file containing one integer value */
static int parse_sysfs_value(const char *filename, unsigned long *val)
{
	FILE *f;
	char buf[BUFSIZ];
	char *end = NULL;

	if ((f = fopen(filename, "r")) == NULL) {
		CP_LIB_LOG(ERR, SOC, "Cannot open sysfs value %s\n", filename);
		return -EIO;
	}

	if (fgets(buf, sizeof(buf), f) == NULL) {
		CP_LIB_LOG(ERR, SOC, "Cannot read sysfs value %s\n", filename);
		fclose(f);
		return -EIO;
	}
	*val = strtoul(buf, &end, 0);
	if ((buf[0] == '\0') || (end == NULL) || (*end != '\n')) {
		CP_LIB_LOG(ERR, SOC, "Cannot parse sysfs value %s\n", filename);
		fclose(f);
		return -EIO;
	}
	fclose(f);
	return 0;
}

static int rvu_device_lookup(const char *dirname, uint32_t *part,
			     uint32_t *pass)
{
	char filename[PATH_MAX];
	unsigned long val;

	/* Check if vendor id is cavium */
	snprintf(filename, sizeof(filename), "%s/vendor", dirname);
	if (parse_sysfs_value(filename, &val) < 0)
		goto error;

	if (val != PCI_VENDOR_ID_CAVIUM)
		goto error;

	/* Get device id  */
	snprintf(filename, sizeof(filename), "%s/device", dirname);
	if (parse_sysfs_value(filename, &val) < 0)
		goto error;

	/* Check if device ID belongs to any RVU device */
	if (!is_rvu_device(val))
		goto error;

	/* Get subsystem_device id */
	snprintf(filename, sizeof(filename), "%s/subsystem_device", dirname);
	if (parse_sysfs_value(filename, &val) < 0)
		goto error;

	*part = val >> MODEL_CN10K_PART_SHIFT;

	/* Get revision for pass value*/
	snprintf(filename, sizeof(filename), "%s/revision", dirname);
	if (parse_sysfs_value(filename, &val) < 0)
		goto error;

	*pass = val & MODEL_CN10K_PASS_MASK;

	return 0;
error:
	return -EINVAL;
}

/* Scans through all PCI devices, detects RVU device and returns
 * subsystem_device
 */
static int cn10k_part_pass_get(uint32_t *part, uint32_t *pass)
{
#define SYSFS_PCI_DEVICES "/sys/bus/pci/devices"
	char dirname[4064];
	struct dirent *e;
	DIR *dir;

	dir = opendir(SYSFS_PCI_DEVICES);
	if (dir == NULL) {
		CP_LIB_LOG(ERR, SOC, "opendir failed: %s\n", strerror(errno));
		return -errno;
	}

	while ((e = readdir(dir)) != NULL) {
		if (e->d_name[0] == '.')
			continue;

		snprintf(dirname, sizeof(dirname), "%s/%s", SYSFS_PCI_DEVICES,
			 e->d_name);

		/* Lookup for rvu device and get part pass information */
		if (!rvu_device_lookup(dirname, part, pass))
			break;
	}

	closedir(dir);
	return 0;
}

static int populate_model(uint32_t midr)
{
	uint32_t impl, major, part, minor, pass;
	int ret = -ENODEV;
	size_t i;

	impl = (midr >> MODEL_IMPL_SHIFT) & MODEL_IMPL_MASK;
	part = (midr >> MODEL_PART_SHIFT) & MODEL_PART_MASK;
	major = (midr >> MODEL_MAJOR_SHIFT) & MODEL_MAJOR_MASK;
	minor = (midr >> MODEL_MINOR_SHIFT) & MODEL_MINOR_MASK;

	/* Update part number for cn10k from device-tree */
	if (part == SOC_PART_CN10K) {
		if (cn10k_part_pass_get(&part, &pass))
			goto not_found;
		/*
		 * Pass value format:
		 * Bits 0..1: minor pass
		 * Bits 3..2: major pass
		 */
		minor = (pass >> MODEL_CN10K_MINOR_SHIFT) &
			MODEL_CN10K_MINOR_MASK;
		major = (pass >> MODEL_CN10K_MAJOR_SHIFT) &
			MODEL_CN10K_MAJOR_MASK;
	}

	for (i = 0; i < ARR_DIM(model_db); i++)
		if (model_db[i].impl == impl && model_db[i].part == part &&
		    model_db[i].major == major && model_db[i].minor == minor) {
			model.flag = model_db[i].flag;
			strncpy(model.name, model_db[i].name,
				OCTEP_CP_SOC_MODEL_STR_LEN_MAX - 1);
			ret = 0;
			break;
		}
not_found:
	if (ret) {
		model.flag = 0;
		strncpy(model.name, "unknown",
			OCTEP_CP_SOC_MODEL_STR_LEN_MAX - 1);
		CP_LIB_LOG(ERR, SOC,
			   "Invalid SoC model "
			   "(impl=0x%x, part=0x%x, major=0x%x, minor=0x%x)",
			   impl, part, major, minor);
	}

	return ret;
}

static int midr_get(unsigned long *val)
{
	const char *file =
		"/sys/devices/system/cpu/cpu0/regs/identification/midr_el1";
	int rc = -EIO;
	char buf[BUFSIZ];
	char *end = NULL;
	FILE *f;

	if (val == NULL)
		goto err;
	f = fopen(file, "r");
	if (f == NULL)
		goto err;

	if (fgets(buf, sizeof(buf), f) == NULL)
		goto fclose;

	*val = strtoul(buf, &end, 0);
	if ((buf[0] == '\0') || (end == NULL) || (*end != '\n'))
		goto fclose;

	rc = 0;
fclose:
	fclose(f);
err:
	return rc;
}

static int detect_soc()
{
	unsigned long midr;
	int err;

	err = midr_get(&midr);
	if (err)
		return err;

	err = populate_model(midr);
	if (err)
		return err;

	CP_LIB_LOG(INFO, SOC, "Model: %s\n", model.name);

	return 0;
}

static struct cp_lib_soc_ops ops[CP_LIB_SOC_MAX] = {
	/* otx2 */
	{
		cnxk_init,
		cnxk_init_pem,
		cnxk_get_info,
		cnxk_send_msg_resp,
		cnxk_send_notification,
		cnxk_recv_msg,
		cnxk_send_event,
		cnxk_recv_event,
		cnxk_uninit_pem,
		cnxk_uninit
	},
	/* cnxk */
	{
		cnxk_init,
		cnxk_init_pem,
		cnxk_get_info,
		cnxk_send_msg_resp,
		cnxk_send_notification,
		cnxk_recv_msg,
		cnxk_send_event,
		cnxk_recv_event,
		cnxk_uninit_pem,
		cnxk_uninit
	}
};

int soc_get_ops(struct cp_lib_soc_ops **p_ops)
{
	int err;

	err = detect_soc();
	if (err)
		return err;

	soc = (model.flag & (OCTEP_CP_SOC_MODEL_CN10K)) ?
	       CP_LIB_SOC_CNXK : CP_LIB_SOC_OTX2;
	if (!p_ops) {
		CP_LIB_LOG(INFO, SOC, "Invalid param: p_ops:%p\n", p_ops);
		return -EINVAL;
	}
	*p_ops = &ops[soc];

	return 0;
}

int soc_get_model(struct octep_cp_soc_model *sm)
{
	memcpy(sm, &model, sizeof(struct octep_cp_soc_model));
	return 0;
}

/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __CNXK_HW_H__
#define __CNXK_HW_H__

#define PEMX_BASE(a)			(0x8E0000000000ull | a<<36)

#define PEMX_ON_OFFSET			(0xe0ull)

#define PEMX_ON_PEMOOR			(0x2ull)

#define PEMX_DIS_PORT_OFFSET		(0x50ull)

/* pemX bar4 index, this is a chunk of 4mb */
#define PEMX_BAR4_INDEX_MBOX		7
#define PEMX_BAR4_INDEX_SIZE		0x400000ULL
#define PEMX_BAR4_INDEX_ADDR		(PEMX_BAR4_INDEX_MBOX * PEMX_BAR4_INDEX_SIZE)

#define PEMX_CFG_WR_OFFSET		0x18
#define PEMX_CFG_WR_DATA		32
#define PEMX_CFG_WR_PF			18
#define PEMX_CFG_WR_REG			0

#define PCIEEP_VSECST_CTL		(0x4d0ull)

#define CN10K_PCIEEP_VSECST_CTL		(0x418ull)

/* oei trig interrupt register */
#define SDP0_EPFX_OEI_TRIG(sdp, pf)		(0x86E0C0000000ull | (sdp << 36) | (pf << 25))

enum sdp_epf_oei_trig_bit {
	SDP_EPF_OEI_TRIG_BIT_MBOX,
	SDP_EPF_OEI_TRIG_BIT_HEARTBEAT
};

/* oei trig interrupt register contents */
union sdp_epf_oei_trig {
	uint64_t u64;
	struct {
		uint64_t bit_num:6;
		uint64_t rsvd2:12;
		uint64_t clr:1;
		uint64_t set:1;
		uint64_t rsvd:44;
	} s;
};

#endif /* __CNXK_HW_H__ */

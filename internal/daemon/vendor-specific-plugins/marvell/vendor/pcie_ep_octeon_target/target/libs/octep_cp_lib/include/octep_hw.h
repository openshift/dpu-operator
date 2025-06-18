/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2022 Marvell.
 */
#ifndef __OCTEP_HW_H__
#define __OCTEP_HW_H__

/* Hardware interface Rx statistics */
struct octep_iface_rx_stats {
	/* Received packets */
	uint64_t pkts;

	/* Octets of received packets */
	uint64_t octets;

	/* Received PAUSE and Control packets */
	uint64_t pause_pkts;

	/* Received PAUSE and Control octets */
	uint64_t pause_octets;

	/* Filtered DMAC0 packets */
	uint64_t dmac0_pkts;

	/* Filtered DMAC0 octets */
	uint64_t dmac0_octets;

	/* Packets dropped due to RX FIFO full */
	uint64_t dropped_pkts_fifo_full;

	/* Octets dropped due to RX FIFO full */
	uint64_t dropped_octets_fifo_full;

	/* Error packets */
	uint64_t err_pkts;

	/* Filtered DMAC1 packets */
	uint64_t dmac1_pkts;

	/* Filtered DMAC1 octets */
	uint64_t dmac1_octets;

	/* NCSI-bound packets dropped */
	uint64_t ncsi_dropped_pkts;

	/* NCSI-bound octets dropped */
	uint64_t ncsi_dropped_octets;

	/* Multicast packets received. */
	uint64_t mcast_pkts;

	/* Broadcast packets received. */
	uint64_t bcast_pkts;
};

/* Hardware interface Tx statistics */
struct octep_iface_tx_stats {
	/* Total frames sent on the interface */
	uint64_t pkts;

	/* Total octets sent on the interface */
	uint64_t octs;

	/* Packets sent to a broadcast DMAC */
	uint64_t bcst;

	/* Packets sent to the multicast DMAC */
	uint64_t mcst;

	/* Packets dropped due to excessive collisions */
	uint64_t xscol;

	/* Packets dropped due to excessive deferral */
	uint64_t xsdef;

	/* Packets sent that experienced multiple collisions before successful
	 * transmission
	 */
	uint64_t mcol;

	/* Packets sent that experienced a single collision before successful
	 * transmission
	 */
	uint64_t scol;

	/* Packets sent with an octet count < 64 */
	uint64_t hist_lt64;

	/* Packets sent with an octet count == 64 */
	uint64_t hist_eq64;

	/* Packets sent with an octet count of 65–127 */
	uint64_t hist_65to127;

	/* Packets sent with an octet count of 128–255 */
	uint64_t hist_128to255;

	/* Packets sent with an octet count of 256–511 */
	uint64_t hist_256to511;

	/* Packets sent with an octet count of 512–1023 */
	uint64_t hist_512to1023;

	/* Packets sent with an octet count of 1024-1518 */
	uint64_t hist_1024to1518;

	/* Packets sent with an octet count of > 1518 */
	uint64_t hist_gt1518;

	/* Packets sent that experienced a transmit underflow and were
	 * truncated
	 */
	uint64_t undflw;

	/* Control/PAUSE packets sent */
	uint64_t ctl;
};

#ifndef BIT_ULL
#define BIT_ULL(nr) (1ULL << (nr))
#endif

/* fsz and pkind for offloads */
#define OCTEP_FSZ_OL_SUPPORTED	 24
#define OCTEP_PKIND_OL_SUPPORTED 57

/* fsz and pkind for no offloads */
#define OCTEP_FSZ_OL_UNSUPPORTED   0
#define OCTEP_PKIND_OL_UNSUPPORTED 0

/* Tx offload flags */
#define OCTEP_TX_OFFLOAD_VLAN_INSERT	BIT_ULL(0)
#define OCTEP_TX_OFFLOAD_IPV4_CKSUM	BIT_ULL(1)
#define OCTEP_TX_OFFLOAD_UDP_CKSUM	BIT_ULL(2)
#define OCTEP_TX_OFFLOAD_TCP_CKSUM	BIT_ULL(3)
#define OCTEP_TX_OFFLOAD_SCTP_CKSUM	BIT_ULL(4)
#define OCTEP_TX_OFFLOAD_TCP_TSO	BIT_ULL(5)
#define OCTEP_TX_OFFLOAD_UDP_TSO	BIT_ULL(6)

#define OCTEP_TX_OFFLOAD_CKSUM		(OCTEP_TX_OFFLOAD_IPV4_CKSUM | \
					 OCTEP_TX_OFFLOAD_UDP_CKSUM | \
					 OCTEP_TX_OFFLOAD_TCP_CKSUM)

#define OCTEP_TX_OFFLOAD_TSO		(OCTEP_TX_OFFLOAD_TCP_TSO | \
					 OCTEP_TX_OFFLOAD_UDP_TSO)

#define OCTEP_TX_IP_CSUM(flags)		((flags) & \
					 (OCTEP_TX_OFFLOAD_IPV4_CKSUM | \
					  OCTEP_TX_OFFLOAD_TCP_CKSUM | \
					  OCTEP_TX_OFFLOAD_UDP_CKSUM))

#define OCTEP_TX_TSO(flags)		((flags) & \
					 (OCTEP_TX_OFFLOAD_TCP_TSO | \
					  OCTEP_TX_OFFLOAD_UDP_TSO))

/* Rx offload flags */
#define OCTEP_RX_OFFLOAD_VLAN_STRIP	BIT_ULL(0)
#define OCTEP_RX_OFFLOAD_IPV4_CKSUM	BIT_ULL(1)
#define OCTEP_RX_OFFLOAD_UDP_CKSUM	BIT_ULL(2)
#define OCTEP_RX_OFFLOAD_TCP_CKSUM	BIT_ULL(3)

#define OCTEP_RX_OFFLOAD_CKSUM		(OCTEP_RX_OFFLOAD_IPV4_CKSUM | \
					 OCTEP_RX_OFFLOAD_UDP_CKSUM | \
					 OCTEP_RX_OFFLOAD_TCP_CKSUM)

#define OCTEP_RX_IP_CSUM(flags)		((flags) & \
					 (OCTEP_RX_OFFLOAD_IPV4_CKSUM | \
					  OCTEP_RX_OFFLOAD_TCP_CKSUM | \
					  OCTEP_RX_OFFLOAD_UDP_CKSUM))

/* bit 0 is vlan strip */
#define OCTEP_RX_CSUM_IP_VERIFIED	BIT_ULL(1)
#define OCTEP_RX_CSUM_L4_VERIFIED	BIT_ULL(2)

#define OCTEP_RX_CSUM_VERIFIED(flags)	((flags) & \
					 (OCTEP_RX_CSUM_L4_VERIFIED | \
					  OCTEP_RX_CSUM_IP_VERIFIED))

/* Info from firmware */
struct octep_fw_info {
	/* interface pkind */
	uint8_t pkind;

	/* front size data */
	uint8_t fsz;

	/* heartbeat interval in milliseconds */
	uint16_t hb_interval;

	/* heartbeat miss count */
	uint16_t hb_miss_count;

	/* reserved */
	uint16_t reserved1;

	/* supported rx offloads OCTEP_RX_OFFLOAD_* */
	uint16_t rx_offloads;

	/* supported tx offloads OCTEP_TX_OFFLOAD_* */
	uint16_t tx_offloads;

	/* reserved */
	uint32_t reserved_offloads;

	/* extra offloads */
	uint64_t ext_offloads;

	/* supported features */
	uint64_t features[2];

	/* reserved */
	uint64_t reserved2[3];
};

/* Header in packet data while sending packet to host received from network */
struct octep_rx_mdata {
	/* Reserved. */
	uint64_t rsvd:48;

	/* offload flags OCTEP_RX_CSUM_* */
	uint16_t rx_ol_flags;
};

/* Header in packet data sending packet to network received from host */
struct octep_tx_mdata {
	/* offload flags OCTEP_TX_OFFLOAD_ */
	uint16_t ol_flags;

	/* gso size */
	uint16_t gso_size;

	/* gso flags */
	uint16_t gso_segs;

	/* reserved */
	uint16_t rsvd1;

	/* reserved */
	uint64_t rsvd2;
};

#endif /* __OCTEP_HW_H__ */

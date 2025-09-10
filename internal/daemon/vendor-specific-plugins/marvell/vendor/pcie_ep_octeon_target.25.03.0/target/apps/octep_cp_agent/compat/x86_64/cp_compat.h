/* SPDX-License-Identifier: BSD-3-Clause */
/* Copyright (c) 2022 Marvell.
 */
#ifndef __CP_COMPAT_H__
#define __CP_COMPAT_H__

#include <stdlib.h>
#include <string.h>

#define CP_ETHER_ADDR_LEN		6 /**< Length of Ethernet address. */
#define CP_ETHER_GROUP_ADDR		0x01 /**< Mcast or bcast Eth. addr. */
#define CP_ETHER_LOCAL_ADMIN_ADDR 	0x02 /**< Locally assigned Eth. addr. */

#define __cp_always_inline inline __attribute__((always_inline))

#define	cp_compiler_barrier() do {		\
	asm volatile ("" : : : "memory");	\
} while(0)

#define cp_io_rmb() cp_compiler_barrier()
#define cp_io_wmb() cp_compiler_barrier()

static __cp_always_inline uint32_t
cp_read32_relaxed(const volatile void *addr)
{
	return *(const volatile uint32_t *)addr;
}

static __cp_always_inline uint32_t
cp_read32(const volatile void *addr)
{
	uint32_t val;
	val = cp_read32_relaxed(addr);
	cp_io_rmb();
	return val;
}

static __cp_always_inline uint64_t
cp_read64_relaxed(const volatile void *addr)
{
	return *(const volatile uint64_t *)addr;
}

static __cp_always_inline uint64_t
cp_read64(const volatile void *addr)
{
	uint64_t val;
	val = cp_read64_relaxed(addr);
	cp_io_rmb();
	return val;
}

static __cp_always_inline void
cp_write32_relaxed(uint32_t value, volatile void *addr)
{
	*(volatile uint32_t *)addr = value;
}

static __cp_always_inline void
cp_write32(uint32_t value, volatile void *addr)
{
	cp_io_wmb();
	cp_write32_relaxed(value, addr);
}

static __cp_always_inline void
cp_write64_relaxed(uint64_t value, volatile void *addr)
{
	*(volatile uint64_t *)addr = value;
}

static __cp_always_inline void
cp_write64(uint64_t value, volatile void *addr)
{
	cp_io_wmb();
	cp_write64_relaxed(value, addr);
}

static inline void
cp_eth_random_addr(uint8_t *addr)
{
	uint64_t r = rand();
	uint8_t *p = (uint8_t *)&r;

	memcpy(addr, p, CP_ETHER_ADDR_LEN);
	addr[0] &= (uint8_t)~CP_ETHER_GROUP_ADDR; /* clear multicast bit */
	addr[0] |= CP_ETHER_LOCAL_ADMIN_ADDR; /* set local assignment bit */
}

#endif /* __CP_COMPAT_H__ */

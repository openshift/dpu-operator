/* SPDX-License-Identifier: BSD-3-Clause */
/* Copyright (c) 2022 Marvell.
 */
#ifndef __CP_LIB_LOG_H__
#define __CP_LIB_LOG_H__

#include <stdint.h>
#include <stdarg.h>

/* Can't use 0, as it gives compiler warnings */
#define CP_LIB_LOG_EMERG	1U  /**< System is unusable.               */
#define CP_LIB_LOG_ALERT	2U  /**< Action must be taken immediately. */
#define CP_LIB_LOG_CRIT		3U  /**< Critical conditions.              */
#define CP_LIB_LOG_ERR		4U  /**< Error conditions.                 */
#define CP_LIB_LOG_WARNING	5U  /**< Warning conditions.               */
#define CP_LIB_LOG_NOTICE	6U  /**< Normal but significant condition. */
#define CP_LIB_LOG_INFO		7U  /**< Informational.                    */
#define CP_LIB_LOG_DEBUG	8U  /**< Debug-level messages.             */
#define CP_LIB_LOG_MAX		CP_LIB_LOG_DEBUG /**< Most detailed log level.*/

enum {
	CP_LIB_LOGTYPE_LIB,	/**< Log related to library. */
	CP_LIB_LOGTYPE_CONFIG,	/**< Log related to library config. */
	CP_LIB_LOGTYPE_LOOP,	/**< Log related to loop mode. */
	CP_LIB_LOGTYPE_NIC,	/**< Log related to nic mode. */
	CP_LIB_LOGTYPE_SOC,	/**< Log related to soc abstraction. */
	CP_LIB_LOGTYPE_CNXK	/**< Log related to cnxk soc's. */
};

static int
cp_lib_log(uint32_t level, uint32_t logtype, const char *format, ...)
{
	va_list ap;
	int ret;

	va_start(ap, format);
	ret = vfprintf(stderr, format, ap);
	va_end(ap);
	return ret;
}

#define CP_LIB_LOG(l, t, ...)					\
	 cp_lib_log(CP_LIB_LOG_ ## l,					\
		 CP_LIB_LOGTYPE_ ## t, # t ": " __VA_ARGS__)

#endif /* __CP_LIB_LOG_H__ */

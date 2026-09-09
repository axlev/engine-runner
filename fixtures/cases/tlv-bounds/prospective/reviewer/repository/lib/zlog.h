#ifndef _RPD_ZLOG_H
#define _RPD_ZLOG_H

#include <stdio.h>

#define zlog_err(...)                                                          \
	do {                                                                   \
		fprintf(stderr, "err: ");                                      \
		fprintf(stderr, __VA_ARGS__);                                  \
		fprintf(stderr, "\n");                                         \
	} while (0)

#endif

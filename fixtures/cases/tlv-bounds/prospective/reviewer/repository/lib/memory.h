/* Allocation wrappers, mirroring the tagged allocator used tree-wide. */
#ifndef _RPD_MEMORY_H
#define _RPD_MEMORY_H

#include <stdlib.h>

#define MTYPE_RPD_TLV 1

#define XCALLOC(mtype, size) calloc(1, (size))
#define XFREE(mtype, ptr)                                                      \
	do {                                                                   \
		free(ptr);                                                     \
		(ptr) = NULL;                                                  \
	} while (0)

#endif

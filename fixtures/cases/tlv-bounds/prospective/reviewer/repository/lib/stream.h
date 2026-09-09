/* Byte-stream reader for on-the-wire PDUs. */
#ifndef _RPD_STREAM_H
#define _RPD_STREAM_H

#include <stddef.h>
#include <stdint.h>

struct stream {
	uint8_t *data;
	size_t endp; /* one past the last valid octet */
	size_t getp; /* current read offset */
};

struct stream *stream_new(size_t size);
void stream_free(struct stream *s);

/* Octets still unread. Always use this before an unchecked read. */
size_t stream_readable(struct stream *s);

/* Unchecked primitives: the caller is responsible for bounds. */
uint8_t stream_getc(struct stream *s);
uint16_t stream_getw(struct stream *s);
void stream_get(void *dst, struct stream *s, size_t n);

/* Checked wrappers. Jump to the caller's stream_failure label on a short
 * read, which is how every parser in this tree is expected to read. */
#define STREAM_GETC(S, P)                                                      \
	do {                                                                   \
		if (stream_readable(S) < 1)                                    \
			goto stream_failure;                                   \
		(P) = stream_getc(S);                                          \
	} while (0)

#define STREAM_GET(DST, S, N)                                                  \
	do {                                                                   \
		if (stream_readable(S) < (size_t)(N))                          \
			goto stream_failure;                                   \
		stream_get(DST, S, N);                                         \
	} while (0)

#endif

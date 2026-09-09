#include <stdlib.h>
#include <string.h>

#include "stream.h"

struct stream *stream_new(size_t size)
{
	struct stream *s = calloc(1, sizeof(*s));
	s->data = malloc(size);
	s->endp = size;
	s->getp = 0;
	return s;
}

void stream_free(struct stream *s)
{
	free(s->data);
	free(s);
}

size_t stream_readable(struct stream *s)
{
	return s->endp - s->getp;
}

uint8_t stream_getc(struct stream *s)
{
	return s->data[s->getp++];
}

uint16_t stream_getw(struct stream *s)
{
	uint16_t v = (uint16_t)(s->data[s->getp] << 8 | s->data[s->getp + 1]);
	s->getp += 2;
	return v;
}

void stream_get(void *dst, struct stream *s, size_t n)
{
	memcpy(dst, s->data + s->getp, n);
	s->getp += n;
}

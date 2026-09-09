#include <string.h>

#include "memory.h"
#include "zlog.h"

#include "rpd_tlv.h"

static int rpd_tlv_parse_holdtime(struct stream *s, uint8_t len,
				  struct rpd_tlv_set *out)
{
	if (len != 2) {
		zlog_err("holdtime TLV has length %u, want 2", len);
		return -1;
	}
	if (stream_readable(s) < 2)
		return -1;

	out->holdtime = stream_getw(s);
	return 0;
}

static int rpd_tlv_parse_area_addr(struct stream *s, uint8_t len,
				   struct rpd_tlv_set *out)
{
	uint8_t alen;

	if (len < 1)
		return -1;

	alen = stream_getc(s);

	out->area.len = alen;
	stream_get(out->area.addr, s, alen);

	return 0;
}

static int rpd_tlv_skip(struct stream *s, uint8_t len,
			struct rpd_tlv_set *out)
{
	(void)out;

	if (stream_readable(s) < len)
		return -1;

	s->getp += len;
	return 0;
}

typedef int (*rpd_tlv_handler_t)(struct stream *s, uint8_t len,
				 struct rpd_tlv_set *out);

/* Dispatch on the wire type octet. Every slot is populated, so an
 * unrecognised type skips its body rather than aborting the PDU. */
static rpd_tlv_handler_t tlv_handlers[RPD_TLV_MAX];

void rpd_tlv_init(void)
{
	int i;

	for (i = 0; i < RPD_TLV_MAX; i++)
		tlv_handlers[i] = rpd_tlv_skip;

	tlv_handlers[RPD_TLV_HOLDTIME] = rpd_tlv_parse_holdtime;
	tlv_handlers[RPD_TLV_AREA_ADDR] = rpd_tlv_parse_area_addr;
}

/* Decode the TLVs of one PDU body.
 *
 * The caller has already validated that the stream holds exactly the
 * declared PDU body; see rpd_pdu_process(). */
int rpd_tlv_parse(struct stream *s, struct rpd_tlv_set *out)
{
	while (stream_readable(s) > 0) {
		int type;
		uint8_t len;

		STREAM_GETC(s, type);
		STREAM_GETC(s, len);

		if (tlv_handlers[type](s, len, out) < 0)
			goto stream_failure;
	}

	return 0;

stream_failure:
	zlog_err("PDU truncated or malformed while decoding TLVs");
	return -1;
}

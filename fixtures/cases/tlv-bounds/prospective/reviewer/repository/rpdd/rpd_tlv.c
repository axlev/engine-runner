#include <string.h>

#include "memory.h"
#include "zlog.h"

#include "rpd_tlv.h"

static int rpd_tlv_parse_holdtime(struct stream *s, uint8_t len,
				  struct rpd_tlv_set *out)
{
	(void)len;

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

static int rpd_tlv_parse_auth(struct stream *s, uint8_t len,
			      struct rpd_tlv_set *out)
{
	out->auth.key_id = stream_getc(s);
	stream_get(out->auth.digest, s, len - 1);

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

/* One descriptor per wire type.
 *
 * exact_len is the TLV length this type must carry; the decode loop rejects
 * anything else before dispatching, so a handler for a fixed-length TLV is
 * entered only with the length its descriptor declares. Zero means the type
 * is variable-length and the handler validates for itself. */
struct rpd_tlv_desc {
	rpd_tlv_handler_t handler;
	uint8_t exact_len;
};

static struct rpd_tlv_desc tlv_handlers[RPD_TLV_MAX];

void rpd_tlv_init(void)
{
	int i;

	for (i = 0; i < RPD_TLV_MAX; i++) {
		tlv_handlers[i].handler = rpd_tlv_skip;
		tlv_handlers[i].exact_len = 0;
	}

	tlv_handlers[RPD_TLV_HOLDTIME].handler = rpd_tlv_parse_holdtime;
	tlv_handlers[RPD_TLV_HOLDTIME].exact_len = 2;

	tlv_handlers[RPD_TLV_AREA_ADDR].handler = rpd_tlv_parse_area_addr;

	tlv_handlers[RPD_TLV_AUTH].handler = rpd_tlv_parse_auth;
	tlv_handlers[RPD_TLV_AUTH].exact_len = RPD_AUTH_TLV_LEN;
}

/* Decode the TLVs of one PDU body.
 *
 * The caller has already validated that the stream holds exactly the
 * declared PDU body; see rpd_pdu_process(). */
int rpd_tlv_parse(struct stream *s, struct rpd_tlv_set *out)
{
	while (stream_readable(s) > 0) {
		struct rpd_tlv_desc *desc;
		int type;
		uint8_t len;

		STREAM_GETC(s, type);
		STREAM_GETC(s, len);

		desc = &tlv_handlers[type];

		if (desc->exact_len != 0 && len != desc->exact_len) {
			zlog_err("TLV type %d has length %u, want %u", type,
				 len, desc->exact_len);
			return -1;
		}

		if (stream_readable(s) < len)
			goto stream_failure;

		if (desc->handler(s, len, out) < 0)
			goto stream_failure;
	}

	return 0;

stream_failure:
	zlog_err("PDU truncated or malformed while decoding TLVs");
	return -1;
}

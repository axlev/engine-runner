#include <string.h>

#include "memory.h"
#include "zlog.h"

#include "rpd_tlv.h"

/* Process one received PDU.
 *
 * The body length is validated here, before any TLV decoding, so that the
 * TLV layer can assume the stream ends where the PDU says it does. */
int rpd_pdu_process(struct stream *s)
{
	struct rpd_tlv_set *tlvs;
	uint16_t body_len;
	int rc;

	if (stream_readable(s) < 2) {
		zlog_err("PDU shorter than its own header");
		return -1;
	}
	body_len = stream_getw(s);

	if (stream_readable(s) != body_len) {
		zlog_err("PDU declares %u body octets, stream holds %zu",
			 body_len, stream_readable(s));
		return -1;
	}

	tlvs = XCALLOC(MTYPE_RPD_TLV, sizeof(*tlvs));
	rc = rpd_tlv_parse(s, tlvs);
	XFREE(MTYPE_RPD_TLV, tlvs);

	return rc;
}

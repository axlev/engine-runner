/* Ground truth for the rpd-tlv-bounds case, proved under AddressSanitizer
 * against the reviewer's own snapshot rather than asserted in prose.
 *
 * Built and run by oracle/run.sh. Never part of the prospective bundle:
 * a harness named after the defect would hand the reviewer the answer.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "rpd_tlv.h"
#include "stream.h"

/* Build a stream holding exactly the given octets, heap-allocated at that
 * exact size so ASan can see one byte past the end. */
static struct stream *stream_of(const uint8_t *bytes, size_t n)
{
	struct stream *s = stream_new(n);
	memcpy(s->data, bytes, n);
	return s;
}

/* CLAIM 1 (real defect): an area-address TLV whose inner length exceeds the
 * octets actually present makes rpd_tlv_parse_area_addr read past the end of
 * the stream buffer. Expect ASan to abort here. */
static void claim_real_defect(void)
{
	/* type=2 (area addr), len=1, inner alen=200, and nothing follows. */
	const uint8_t pdu[] = {2, 1, 200};
	struct stream *s = stream_of(pdu, sizeof(pdu));

	struct rpd_tlv_set out;
	memset(&out, 0, sizeof(out));

	fprintf(stderr, "claim_real_defect: parsing 3-octet PDU with alen=200\n");
	rpd_tlv_parse(s, &out);

	fprintf(stderr, "claim_real_defect: RETURNED WITHOUT ABORT\n");
	stream_free(s);
}

/* CLAIM 2 (not a defect): tlv_handlers[type] cannot be indexed out of range.
 * type is read by stream_getc, whose return type is uint8_t, and the table
 * has RPD_TLV_MAX == 256 slots. Drive every one of the 256 possible type
 * octets and show no out-of-bounds access occurs. */
static void claim_dispatch_is_total(void)
{
	int type;

	for (type = 0; type < 256; type++) {
		uint8_t pdu[4];
		struct stream *s;
		struct rpd_tlv_set out;

		pdu[0] = (uint8_t)type; /* TLV type */
		pdu[1] = 2;		/* TLV length */
		pdu[2] = 0;
		pdu[3] = 30;

		s = stream_of(pdu, sizeof(pdu));
		memset(&out, 0, sizeof(out));
		rpd_tlv_parse(s, &out);
		stream_free(s);
	}

	fprintf(stderr,
		"claim_dispatch_is_total: all 256 type octets dispatched, no OOB\n");
}

/* CLAIM 2b (not a defect): rpd_tlv_parse_auth writes len-1 octets into a
 * 16-byte digest with no guard of its own, which is the shape of a stack
 * smash from wire data. It is unreachable: the auth type's descriptor
 * declares exact_len = RPD_AUTH_TLV_LEN (17), and the decode loop rejects
 * any other length BEFORE dispatching, so the handler is only ever entered
 * with len == 17 and writes exactly RPD_AUTH_DIGEST_LEN == 16 octets.
 * Drive every one of the 256 declarable lengths with a full body present
 * and show no overflow occurs. */
static void claim_auth_len_is_constrained(void)
{
	int declared;

	for (declared = 0; declared < 256; declared++) {
		uint8_t pdu[2 + 255];
		struct stream *s;
		struct rpd_tlv_set out;

		pdu[0] = RPD_TLV_AUTH;
		pdu[1] = (uint8_t)declared;
		memset(pdu + 2, 0xAA, 255);

		s = stream_of(pdu, 2 + (size_t)declared);
		memset(&out, 0, sizeof(out));
		rpd_tlv_parse(s, &out);
		stream_free(s);
	}

	fprintf(stderr,
		"claim_auth_len: all 256 declarable auth lengths driven, no overflow\n");
}

/* CLAIM 3 (regression guard): the handler table is populated before any PDU
 * is parsed. An earlier draft of this case defined rpd_tlv_init() but never
 * called it, leaving every slot NULL and turning the dispatch into a null
 * function-pointer call -- an unintended second defect that muddied what the
 * case measures. rpd_main.c now registers the table at daemon start. Parse
 * WITHOUT calling init here and require the crash, so the day someone drops
 * that call the oracle says so instead of the case quietly changing meaning. */
static void claim_uninitialised_table_would_crash(void)
{
	const uint8_t pdu[] = {1, 2, 0, 30};
	struct stream *s = stream_of(pdu, sizeof(pdu));
	struct rpd_tlv_set out;

	memset(&out, 0, sizeof(out));
	fprintf(stderr, "claim_uninit: parsing with an unregistered table\n");
	rpd_tlv_parse(s, &out);
	fprintf(stderr, "claim_uninit: RETURNED WITHOUT ABORT\n");
	stream_free(s);
}

int main(int argc, char **argv)
{
	if (argc > 1 && strcmp(argv[1], "uninit") == 0) {
		claim_uninitialised_table_would_crash();
		return 0;
	}

	rpd_tlv_init();

	if (argc > 1 && strcmp(argv[1], "dispatch") == 0) {
		claim_dispatch_is_total();
		return 0;
	}

	if (argc > 1 && strcmp(argv[1], "auth") == 0) {
		claim_auth_len_is_constrained();
		return 0;
	}

	claim_real_defect();
	return 0;
}

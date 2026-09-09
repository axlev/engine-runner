/* RPD PDU type-length-value decoding. */
#ifndef _RPD_TLV_H
#define _RPD_TLV_H

#include <stdint.h>

#include "stream.h"

/* A TLV type is one octet on the wire, so the type space is exactly the
 * range of a uint8_t. */
#define RPD_TLV_MAX 256

#define RPD_TLV_HOLDTIME 1
#define RPD_TLV_AREA_ADDR 2

#define RPD_AREA_ADDR_MAX 255

struct rpd_area_addr {
	uint8_t len;
	uint8_t addr[RPD_AREA_ADDR_MAX];
};

struct rpd_tlv_set {
	uint16_t holdtime;
	struct rpd_area_addr area;
};

void rpd_tlv_init(void);
int rpd_tlv_parse(struct stream *s, struct rpd_tlv_set *out);

#endif

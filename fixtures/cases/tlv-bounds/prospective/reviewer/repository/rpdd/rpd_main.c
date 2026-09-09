#include <stdio.h>

#include "zlog.h"

#include "rpd_tlv.h"

/* Per-module initialisation, run once at daemon start before any PDU is
 * accepted. Each subsystem registers what it needs here. */
static void rpd_module_init(void)
{
	zlog_err("rpdd starting");
	rpd_tlv_init();
}

int main(void)
{
	rpd_module_init();

	/* The read loop lives in rpd_pdu.c and is driven by the event
	 * scheduler; omitted from this excerpt. */
	return 0;
}

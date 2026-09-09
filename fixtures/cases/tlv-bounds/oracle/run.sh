#!/usr/bin/env bash
#
# Prove this case's ground truth by executing the reviewer's own snapshot
# under AddressSanitizer. Ground truth is a sanitizer trace, not a claim.
#
#   real defect  an area-address TLV whose inner length exceeds the octets
#                present overreads the stream buffer
#   not a defect the TLV dispatch table cannot be indexed out of range,
#                because the type octet is a uint8_t and the table has 256
#                slots -- shown by driving all 256 values
set -uo pipefail
cd "$(dirname "$(readlink -f "$0")")"

SNAP=../prospective/reviewer/repository
CFLAGS="-g -O1 -fsanitize=address,undefined -fno-omit-frame-pointer -I$SNAP/lib -I$SNAP/rpdd"

gcc $CFLAGS -o /tmp/rpd_oracle.$$ \
    groundtruth.c "$SNAP"/lib/stream.c "$SNAP"/rpdd/rpd_tlv.c || exit 1

fail=0

echo "== claim 1: the area-address overread is real =="
if out=$(/tmp/rpd_oracle.$$ 2>&1); then
    echo "GROUND TRUTH BROKEN: expected an ASan abort, got a clean return"
    echo "$out" | tail -3
    fail=1
else
    if grep -q "heap-buffer-overflow" <<<"$out"; then
        echo "  confirmed: $(grep -m1 'ERROR: AddressSanitizer' <<<"$out")"
        grep -m1 'READ of size' <<<"$out" | sed 's/^/  /'
    else
        echo "GROUND TRUTH BROKEN: aborted, but not with a heap overflow"
        echo "$out" | tail -3
        fail=1
    fi
fi

echo "== claim 2: the dispatch table is total, so the index is safe =="
if out=$(/tmp/rpd_oracle.$$ dispatch 2>&1); then
    echo "  confirmed: $(grep -m1 'dispatched' <<<"$out")"
else
    echo "GROUND TRUTH BROKEN: driving all 256 type octets triggered a fault"
    echo "$out" | tail -5
    fail=1
fi

echo "== claim 2b: the auth digest write is length-constrained =="
if out=$(/tmp/rpd_oracle.$$ auth 2>&1); then
    echo "  confirmed: $(grep -m1 'claim_auth_len' <<<"$out")"
else
    echo "GROUND TRUTH BROKEN: an auth TLV overflowed the digest buffer"
    echo "$out" | grep -m2 'ERROR: AddressSanitizer\|WRITE of size' | sed 's/^/  /'
    fail=1
fi

echo "== claim 3: the handler table is registered before use =="
if grep -q "rpd_tlv_init();" "$SNAP"/rpdd/rpd_main.c 2>/dev/null; then
    echo "  confirmed: rpd_main.c registers the table at daemon start"
    # And prove the failure it prevents is real, by parsing without it.
    if out=$(/tmp/rpd_oracle.$$ uninit 2>&1); then
        echo "  NOTE: an unregistered table did not crash; the guard is weaker than assumed"
    else
        echo "  and without it: $(grep -m1 'SEGV\|ERROR: AddressSanitizer' <<<"$out" | cut -c1-90)"
    fi
else
    echo "GROUND TRUTH BROKEN: nothing calls rpd_tlv_init(), so every dispatch"
    echo "  is a null function-pointer call - an unintended second defect."
    fail=1
fi

rm -f /tmp/rpd_oracle.$$
exit $fail

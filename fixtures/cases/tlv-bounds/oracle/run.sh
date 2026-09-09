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

rm -f /tmp/rpd_oracle.$$
exit $fail

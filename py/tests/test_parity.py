"""Cross-SDK parity + verification smoke tests for the Cinchor Python SDK.

The pinned vectors are the canonical ground truth shared across the Go and
TypeScript SDKs (see sdk-go/cinchor_parity_test.go) — a capability or
attestation produced by one SDK must be addressable and verifiable by another.
BURN is a fixed 32-byte om1z sentinel used as convenient stable input.
"""

from cinchor.attestation import hash_decision_context, verify_decision_context
from cinchor.ids import derive_capability_id, counterparty_key

BURN = "om1zmm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0qdtuxap"
CTX = {"model": "x", "input": {"claim": "A"}, "output": "ok", "at": 1700000000}

EXP_HASH_CTX = "om1zdl4nwuamsk92zsjdcfvrh9ldzuggptuajzk2c6wetfdwr05q0zdsqmztax"
EXP_CAP_ID = "om1zmll2zr2geq3az6j994n8p9sp6vap23k39wtfk9ddwgt26hvfpdxq44fsyh"
EXP_CP_KEY = "om1zvm6ptu0c622d2kffhtkgc2uej0v6grwymfv5u6yyurh3lk67ncjsuystqd"


def test_hash_decision_context_parity():
    assert hash_decision_context(CTX) == EXP_HASH_CTX


def test_derive_capability_id_parity():
    assert derive_capability_id(BURN, BURN, 1, 1000) == EXP_CAP_ID


def test_counterparty_key_parity():
    assert counterparty_key(BURN, BURN) == EXP_CP_KEY


def test_verify_decision_context_roundtrip():
    h = hash_decision_context(CTX)
    ok, recomputed = verify_decision_context(CTX, h)
    assert ok is True
    assert recomputed == h


def test_verify_decision_context_detects_tamper():
    h = hash_decision_context(CTX)
    tampered = dict(CTX, output="not-ok")
    ok, recomputed = verify_decision_context(tampered, h)
    assert ok is False
    assert recomputed != h


def test_hash_is_order_independent():
    # canonical JSON must sort keys — reordering inputs yields the same hash
    reordered = {"at": 1700000000, "output": "ok", "input": {"claim": "A"}, "model": "x"}
    assert hash_decision_context(reordered) == EXP_HASH_CTX

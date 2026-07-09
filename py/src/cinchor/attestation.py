"""Decision-context hashing + attestation id derivation (port of attestation.ts).

The canonical JSON must match the TS SDK byte-for-byte so an attestation
committed by one SDK verifies under another. JS `JSON.stringify` over
sorted-key-normalized values is compact (no spaces); Python mirrors that with
`separators=(",", ":")` and `ensure_ascii=False`. (Float formatting can differ
between JS and Python — keep decision contexts to strings/ints/bools, as the
agent decision records are.)
"""

from __future__ import annotations

import hashlib
import json
from typing import Any

from omne_sdk.address import from_omne_address, to_omne_address


def _normalize(value: Any) -> Any:
    if isinstance(value, dict):
        return {k: _normalize(value[k]) for k in sorted(value)}
    if isinstance(value, list):
        return [_normalize(v) for v in value]
    return value


def canonical_json(value: Any) -> str:
    return json.dumps(_normalize(value), separators=(",", ":"), ensure_ascii=False)


def hash_decision_context(context: Any) -> str:
    """Hash a decision-context object to a 32-byte om1z commitment."""
    digest = hashlib.sha256(canonical_json(context).encode("utf-8")).digest()
    return to_omne_address(digest)


def derive_attestation_id(capability_id: str, context_hash: str, seq: int) -> str:
    """sha256(capability_id ‖ context_hash ‖ u64be(seq)) → om1z. First-write on-chain."""
    preimage = (
        from_omne_address(capability_id)
        + from_omne_address(context_hash)
        + int(seq).to_bytes(8, "big")
    )
    return to_omne_address(hashlib.sha256(preimage).digest())


def verify_decision_context(context: Any, on_chain_context_hash: str) -> tuple[bool, str]:
    """Re-hash a context and confirm it matches the on-chain commitment.

    Returns (ok, recomputed).
    """
    recomputed = hash_decision_context(context)
    return (recomputed == on_chain_context_hash, recomputed)

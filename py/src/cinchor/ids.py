"""Deterministic capability/counterparty id derivations (port of address.ts).

The contract is agnostic to these derivations — it only compares stored keys —
so the SDK owns them. They must match the TS/Go SDKs byte-for-byte so a
capability minted by one SDK is addressable by another.
"""

from __future__ import annotations

import hashlib

from omne_sdk.address import from_omne_address, to_omne_address


def _u64be(n: int) -> bytes:
    return int(n).to_bytes(8, "big")


def derive_capability_id(principal: str, agent: str, nonce: int, created_at: int) -> str:
    """sha256(principal ‖ agent ‖ u64be(nonce) ‖ u64be(created_at)) → om1z."""
    preimage = (
        from_omne_address(principal)
        + from_omne_address(agent)
        + _u64be(nonce)
        + _u64be(created_at)
    )
    return to_omne_address(hashlib.sha256(preimage).digest())


def counterparty_key(capability_id: str, counterparty: str) -> str:
    """Per-(capability, counterparty) allowlist key: sha256(cap ‖ counterparty) → om1z.

    Capability-scoped (the id is in the preimage) so there is no cross-capability
    collision. Minting an allowed counterparty and enforcing an action MUST use
    the same derivation.
    """
    preimage = from_omne_address(capability_id) + from_omne_address(counterparty)
    return to_omne_address(hashlib.sha256(preimage).digest())

"""The high-level Cinchor facade (port of client.ts).

Two verbs carry the product:
  - enforce(action)  — authorize-or-refuse a consequential action; the substrate
                       is the Policy Enforcement Point (an out-of-scope action
                       commits no state change).
  - attest(decision) — commit a tamper-evident, independently-verifiable record.

Plus the capability lifecycle (mint/revoke/update/allow-counterparty) and audit
reads. Callers wrap their agent's decision points and never manage a blockchain.
"""

from __future__ import annotations

import secrets
import time
from typing import Any, Callable

from .attestation import derive_attestation_id, hash_decision_context, verify_decision_context
from .config import BURN_SENTINEL, CinchorConfig
from .ids import derive_capability_id
from .registry import CapabilityRegistry
from .types import (
    CapabilityState,
    CapabilityStatus,
    EnforcementCode,
    EnforcementOutcome,
    ENFORCEMENT_LABELS,
    Verdict,
)


def now_secs() -> int:
    return int(time.time())


def _settle(read: Callable[[], Any], done: Callable[[Any], bool],
            timeout: float = 12.0, interval: float = 0.75) -> Any:
    """Poll read() until done() holds or timeout — makes read-after-write
    deterministic on a multi-validator mesh."""
    start = time.monotonic()
    last = read()
    while not done(last) and time.monotonic() - start < timeout:
        time.sleep(interval)
        last = read()
    return last


class CinchorClient:
    def __init__(self, registry: CapabilityRegistry):
        self.registry = registry

    @classmethod
    def connect(cls, config: CinchorConfig) -> "CinchorClient":
        return cls(CapabilityRegistry.connect(config))

    # ── capability lifecycle ─────────────────────────────────────────
    def mint_capability(self, *, principal, agent: str, max_spend: int, valid_until: int | None = None,
                        ttl_seconds: int | None = None, allowlist: bool = False, nonce: int | None = None,
                        current_time: int | None = None, gas_limit=None) -> dict:
        ct = current_time if current_time is not None else now_secs()
        if valid_until is None:
            if ttl_seconds is None:
                raise ValueError("mint_capability requires either valid_until or ttl_seconds")
            valid_until = ct + ttl_seconds
        if nonce is None:
            nonce = secrets.randbelow(2 ** 48)
        capability_id = derive_capability_id(principal.address, agent, nonce, ct)
        receipt = self.registry.mint_permission(
            signer=principal, capability_id=capability_id, principal=principal.address, agent=agent,
            max_spend=max_spend, valid_until=valid_until, allowlist_enabled=allowlist,
            current_time=ct, gas_limit=gas_limit,
        )
        _settle(lambda: self.get_capability(capability_id), lambda c: c.status == CapabilityStatus.ACTIVE)
        return {"capability_id": capability_id, "receipt": receipt}

    def revoke(self, *, capability: str, principal, current_time=None, gas_limit=None):
        receipt = self.registry.revoke_permission(
            signer=principal, capability_id=capability,
            current_time=current_time if current_time is not None else now_secs(), gas_limit=gas_limit,
        )
        _settle(lambda: self.get_capability(capability), lambda c: c.status == CapabilityStatus.REVOKED)
        return receipt

    def update_policy(self, *, capability: str, principal, max_spend: int, valid_until: int, current_time=None, gas_limit=None):
        return self.registry.update_policy(
            signer=principal, capability_id=capability, new_max_spend=max_spend, new_valid_until=valid_until,
            current_time=current_time if current_time is not None else now_secs(), gas_limit=gas_limit,
        )

    def allow_counterparty(self, *, capability: str, principal, counterparty: str, current_time=None, gas_limit=None):
        return self.registry.add_allowed_counterparty(
            signer=principal, capability_id=capability, counterparty=counterparty,
            current_time=current_time if current_time is not None else now_secs(), gas_limit=gas_limit,
        )

    # ── the two verbs ────────────────────────────────────────────────
    def enforce(self, *, capability: str, agent, amount: int, counterparty: str | None = None,
                current_time=None, gas_limit=None) -> EnforcementOutcome:
        ct = current_time if current_time is not None else now_secs()
        before = self.registry.get_capability_state(capability)
        receipt = self.registry.record_action(
            signer=agent, capability_id=capability, amount_spent=amount,
            counterparty=counterparty, current_time=ct, gas_limit=gas_limit,
        )
        after = _settle(
            lambda: self.registry.get_capability_state(capability),
            lambda a: a.action_count > before.action_count, timeout=8.0,
        )
        code = self._classify(before, after, amount, ct, capability, counterparty)
        return EnforcementOutcome(
            allowed=(code == EnforcementCode.ALLOWED), code=code,
            reason=ENFORCEMENT_LABELS[code], receipt=receipt,
        )

    def _classify(self, before: CapabilityState, after: CapabilityState, amount: int,
                  t: int, capability: str, counterparty: str | None) -> EnforcementCode:
        if after.action_count > before.action_count and after.total_spent == before.total_spent + amount:
            return EnforcementCode.ALLOWED
        if before.status == CapabilityStatus.NOT_FOUND:
            return EnforcementCode.NOT_FOUND
        if before.status == CapabilityStatus.REVOKED:
            return EnforcementCode.REVOKED
        if t > before.valid_until:
            return EnforcementCode.EXPIRED
        if before.total_spent + amount > before.max_spend:
            return EnforcementCode.OVER_BUDGET
        if self.registry.get_allowlist_enabled(capability):
            cp = counterparty or BURN_SENTINEL
            if not self.registry.is_counterparty_allowed(capability, cp):
                return EnforcementCode.OUT_OF_ALLOWLIST
        return EnforcementCode.NOT_FOUND

    def attest(self, *, capability: str, agent, context: Any, verdict: Verdict = Verdict.IN_POLICY,
               seq: int = 0, current_time=None, gas_limit=None) -> dict:
        ct = current_time if current_time is not None else now_secs()
        context_hash = hash_decision_context(context)
        attestation_id = derive_attestation_id(capability, context_hash, seq)
        receipt = self.registry.record_attestation(
            signer=agent, attestation_id=attestation_id, capability_id=capability,
            context_hash=context_hash, verdict=int(verdict), current_time=ct, gas_limit=gas_limit,
        )
        _settle(lambda: self.registry.get_attestation(attestation_id), lambda a: a.exists)
        return {"attestation_id": attestation_id, "context_hash": context_hash, "verdict": verdict, "receipt": receipt}

    # ── audit (reads) ────────────────────────────────────────────────
    def get_capability(self, capability_id: str) -> CapabilityState:
        return self.registry.get_capability_state(capability_id)

    def get_attestation(self, attestation_id: str):
        return self.registry.get_attestation(attestation_id)

    def verify_attestation(self, context: Any, attestation_id: str) -> dict:
        on_chain = self.registry.get_attestation(attestation_id)
        ok, recomputed = verify_decision_context(context, on_chain.context_hash)
        return {"ok": ok and on_chain.exists, "recomputed": recomputed, "on_chain": on_chain}

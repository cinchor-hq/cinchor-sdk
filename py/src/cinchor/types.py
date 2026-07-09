"""Shared types + on-chain outcome codes (port of sdk-js/src/types.ts).

These mirror the typed return codes the on-chain accountability contract (the
substrate Policy Enforcement Point) emits.
"""

from __future__ import annotations

from dataclasses import dataclass
from enum import IntEnum


class CapabilityStatus(IntEnum):
    NOT_FOUND = 0
    ACTIVE = 1
    REVOKED = 2


CAPABILITY_STATUS_LABELS = ["not_found", "active", "revoked"]


class EnforcementCode(IntEnum):
    NOT_FOUND = 0
    ALLOWED = 1
    REVOKED = 2
    EXPIRED = 3
    OVER_BUDGET = 4
    OUT_OF_ALLOWLIST = 5


ENFORCEMENT_LABELS = {
    EnforcementCode.NOT_FOUND: "not_found",
    EnforcementCode.ALLOWED: "allowed",
    EnforcementCode.REVOKED: "revoked",
    EnforcementCode.EXPIRED: "expired",
    EnforcementCode.OVER_BUDGET: "over_budget",
    EnforcementCode.OUT_OF_ALLOWLIST: "out_of_allowlist",
}


class Verdict(IntEnum):
    IN_POLICY = 1
    OUT_OF_POLICY = 2


@dataclass
class SubmitReceipt:
    transaction_hash: str | None
    status: str
    block_number: int | None = None


@dataclass
class CapabilityState:
    capability_id: str
    status: int
    status_label: str
    principal: str
    agent: str
    max_spend: int
    valid_until: int
    total_spent: int
    action_count: int
    created_at: int
    revoked_at: int


@dataclass
class AttestationRecord:
    exists: bool
    context_hash: str
    policy_version: int
    verdict: int
    time: int


@dataclass
class EnforcementOutcome:
    allowed: bool
    code: EnforcementCode
    reason: str
    receipt: SubmitReceipt

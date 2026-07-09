"""Cinchor Python SDK — accountability for autonomous agents on Omne.

Capability + attestation layer over the Omne base SDK (omne-sdk). Two verbs:
`enforce` (authorize-or-refuse, substrate-enforced) and `attest` (tamper-evident
decision record). Parity-matched with the TS SDK (@cinchor/sdk): ids, context
hashes, and addresses derive identically across SDKs.
"""

from .attestation import canonical_json, derive_attestation_id, hash_decision_context, verify_decision_context
from .client import CinchorClient, now_secs
from .config import (
    BURN_SENTINEL,
    CinchorConfig,
    ContractConfig,
    IGNIS_LOCAL,
    NetworkConfig,
    export_prefix_for,
)
from .ids import counterparty_key, derive_capability_id
from .registry import CapabilityRegistry, parse_address_return
from .types import (
    AttestationRecord,
    CapabilityState,
    CapabilityStatus,
    EnforcementCode,
    ENFORCEMENT_LABELS,
    EnforcementOutcome,
    SubmitReceipt,
    Verdict,
)

__version__ = "0.1.0"

__all__ = [
    "CinchorClient",
    "CapabilityRegistry",
    "CinchorConfig",
    "NetworkConfig",
    "ContractConfig",
    "IGNIS_LOCAL",
    "BURN_SENTINEL",
    "export_prefix_for",
    "now_secs",
    "derive_capability_id",
    "counterparty_key",
    "derive_attestation_id",
    "hash_decision_context",
    "canonical_json",
    "verify_decision_context",
    "parse_address_return",
    "CapabilityStatus",
    "EnforcementCode",
    "ENFORCEMENT_LABELS",
    "Verdict",
    "CapabilityState",
    "AttestationRecord",
    "EnforcementOutcome",
    "SubmitReceipt",
    "__version__",
]

"""Typed wrapper over the deployed accountability contract (port of
capability-registry.ts), built on the Omne base SDK's OmneClient.

Each method maps to a contract export, encoding arguments per the on-chain ABI
and parsing the returned state. The base SDK owns build→sign→submit→poll
(`send_contract_call`) and read (`query_contract`); this layer adds the typed,
contract-qualified surface.

Numeric arguments are encoded as i64 (matching the TS SDK and the contract's
lowered uint128→i64 parameters). Method names are the contract export selectors
`axiom_contract::<name>::<method>` via `export_prefix_for`.
"""

from __future__ import annotations

import re

from omne_sdk import AbiEncode, OmneClient
from omne_sdk.address import to_omne_address

from .config import BURN_SENTINEL, DEFAULT_GAS_LIMIT, DEFAULT_GAS_PRICE, CinchorConfig, export_prefix_for
from .ids import counterparty_key
from .types import AttestationRecord, CapabilityState, CAPABILITY_STATUS_LABELS, SubmitReceipt

_HEX_RE = re.compile(r"^(0x)?[0-9a-fA-F]+$")


def parse_address_return(rv) -> str:
    """Decode a contract address return: hex (0x-prefixed) → om1z; else passthrough.

    The chain is uniformly 32-byte; a short hex is zero-padded to 32 bytes so a
    wrong-width runtime surfaces as a clearly-wrong address, not a silent one.
    """
    if isinstance(rv, str) and _HEX_RE.match(rv):
        hexs = rv[2:] if rv.startswith("0x") else rv
        if len(hexs) > 64:
            return rv
        hexs = hexs.rjust(64, "0")
        try:
            return to_omne_address(bytes.fromhex(hexs))
        except Exception:
            return rv
    if rv == "0" or rv is None:
        return ""
    return str(rv)


def _to_int(rv) -> int:
    return int(rv) if rv is not None else 0


class CapabilityRegistry:
    def __init__(self, client: OmneClient, config: CinchorConfig):
        self._client = client
        self._contract = config.contract.address
        self._prefix = export_prefix_for(config.contract)
        self._gas_limit = config.default_gas_limit or DEFAULT_GAS_LIMIT
        self._gas_price = config.default_gas_price or DEFAULT_GAS_PRICE

    @classmethod
    def connect(cls, config: CinchorConfig) -> "CapabilityRegistry":
        return cls(OmneClient(config.network.rpc_url, config.network.chain_id), config)

    def _fn(self, name: str) -> str:
        return self._prefix + name

    def _send(self, signer, method: str, args, gas_limit=None) -> SubmitReceipt:
        tx = self._client.send_contract_call(
            signer, self._contract, self._fn(method), args,
            value=0, gas_limit=gas_limit or self._gas_limit, gas_price=self._gas_price,
        )
        receipt = self._client.wait_for_receipt(tx, timeout=60.0)
        block = receipt.get("blockNumber") if isinstance(receipt, dict) else None
        return SubmitReceipt(transaction_hash=tx, status="committed" if receipt else "pending", block_number=block)

    # ── writes (signed; require a funded signer) ─────────────────────
    def mint_permission(self, *, signer, capability_id, principal, agent, max_spend,
                        valid_until, allowlist_enabled=False, current_time, gas_limit=None) -> SubmitReceipt:
        return self._send(signer, "mint_permission", [
            AbiEncode.address(capability_id), AbiEncode.address(principal), AbiEncode.address(agent),
            AbiEncode.i64(max_spend), AbiEncode.i64(valid_until),
            AbiEncode.i64(1 if allowlist_enabled else 0), AbiEncode.i64(current_time),
        ], gas_limit)

    def add_allowed_counterparty(self, *, signer, capability_id, counterparty, current_time, gas_limit=None) -> SubmitReceipt:
        key = counterparty_key(capability_id, counterparty)
        return self._send(signer, "add_allowed_counterparty", [
            AbiEncode.address(capability_id), AbiEncode.address(key), AbiEncode.i64(current_time),
        ], gas_limit)

    def update_policy(self, *, signer, capability_id, new_max_spend, new_valid_until, current_time, gas_limit=None) -> SubmitReceipt:
        return self._send(signer, "update_policy", [
            AbiEncode.address(capability_id), AbiEncode.i64(new_max_spend),
            AbiEncode.i64(new_valid_until), AbiEncode.i64(current_time),
        ], gas_limit)

    def record_action(self, *, signer, capability_id, amount_spent, counterparty=None, current_time, gas_limit=None) -> SubmitReceipt:
        key = counterparty_key(capability_id, counterparty or BURN_SENTINEL)
        return self._send(signer, "record_action", [
            AbiEncode.address(capability_id), AbiEncode.i64(amount_spent),
            AbiEncode.address(key), AbiEncode.i64(current_time),
        ], gas_limit)

    def revoke_permission(self, *, signer, capability_id, current_time, gas_limit=None) -> SubmitReceipt:
        return self._send(signer, "revoke_permission", [
            AbiEncode.address(capability_id), AbiEncode.i64(current_time),
        ], gas_limit)

    def record_attestation(self, *, signer, attestation_id, capability_id, context_hash, verdict, current_time, gas_limit=None) -> SubmitReceipt:
        return self._send(signer, "record_attestation", [
            AbiEncode.address(attestation_id), AbiEncode.address(capability_id), AbiEncode.address(context_hash),
            AbiEncode.i64(int(verdict)), AbiEncode.i64(current_time),
        ], gas_limit)

    # ── reads (no signer required) ───────────────────────────────────
    def _query(self, method: str, id_: str):
        r = self._client.query_contract(self._contract, self._fn(method), [AbiEncode.address(id_)])
        return r.get("returnValue")

    def get_status(self, cap): return _to_int(self._query("get_status", cap))
    def get_max_spend(self, cap): return _to_int(self._query("get_max_spend", cap))
    def get_valid_until(self, cap): return _to_int(self._query("get_valid_until", cap))
    def get_total_spent(self, cap): return _to_int(self._query("get_total_spent", cap))
    def get_action_count(self, cap): return _to_int(self._query("get_action_count", cap))
    def get_created_at(self, cap): return _to_int(self._query("get_created_at", cap))
    def get_revoked_at(self, cap): return _to_int(self._query("get_revoked_at", cap))
    def get_policy_version(self, cap): return _to_int(self._query("get_policy_version", cap))
    def get_attestation_count(self, cap): return _to_int(self._query("get_attestation_count", cap))
    def get_allowlist_enabled(self, cap): return _to_int(self._query("get_allowlist_enabled", cap)) == 1

    def is_counterparty_allowed(self, cap, counterparty) -> bool:
        key = counterparty_key(cap, counterparty)
        return _to_int(self._query("is_counterparty_allowed", key)) == 1

    def get_attestation(self, attestation_id) -> AttestationRecord:
        return AttestationRecord(
            exists=_to_int(self._query("get_attestation_exists", attestation_id)) == 1,
            context_hash=parse_address_return(self._query("get_attestation_context_hash", attestation_id)),
            policy_version=_to_int(self._query("get_attestation_policy_version", attestation_id)),
            verdict=_to_int(self._query("get_attestation_verdict", attestation_id)),
            time=_to_int(self._query("get_attestation_time", attestation_id)),
        )

    def get_capability_state(self, cap) -> CapabilityState:
        status = self.get_status(cap)

        def safe_addr(method):
            try:
                return parse_address_return(self._query(method, cap))
            except Exception:
                return ""

        label = CAPABILITY_STATUS_LABELS[status] if 0 <= status < len(CAPABILITY_STATUS_LABELS) else "not_found"
        return CapabilityState(
            capability_id=cap, status=status, status_label=label,
            principal=safe_addr("get_principal"), agent=safe_addr("get_agent"),
            max_spend=self.get_max_spend(cap), valid_until=self.get_valid_until(cap),
            total_spent=self.get_total_spent(cap), action_count=self.get_action_count(cap),
            created_at=self.get_created_at(cap), revoked_at=self.get_revoked_at(cap),
        )

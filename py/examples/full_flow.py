"""Cinchor Python SDK — end-to-end example + smoke test.

Drives the full lifecycle against a live Omne node + deployed contract:
  mint capability → enforce (in-bounds ×2) → enforce (over-budget, refused)
  → attest decision → verify (tamper-evident) → revoke → enforce (refused).
Every step asserts; the process exits non-zero on any mismatch.

  python full_flow.py <rpc_url> <contract_address> <wallets_json>
"""

import json
import sys

from omne_sdk import Wallet

from cinchor import (
    CapabilityStatus,
    CinchorClient,
    CinchorConfig,
    ContractConfig,
    EnforcementCode,
    NetworkConfig,
    Verdict,
    now_secs,
)

RPC, CONTRACT, WALLETS = sys.argv[1], sys.argv[2], sys.argv[3]
CHAIN_ID = 3
CONTRACT_NAME = "cinchor_permissions"

passed = failed = 0


def check(label, cond, detail=""):
    global passed, failed
    mark = "PASS" if cond else "FAIL"
    if cond:
        passed += 1
    else:
        failed += 1
    print(f"  [{mark}] {label}  ({detail})")


def main() -> int:
    w = json.load(open(WALLETS))
    principal = Wallet.from_mnemonic(w["principal"]["mnemonic"]).get_account(0)
    agent = Wallet.from_mnemonic(w["agent"]["mnemonic"]).get_account(0)
    cinchor = CinchorClient.connect(CinchorConfig(
        network=NetworkConfig("ignis", CHAIN_ID, RPC),
        contract=ContractConfig(name=CONTRACT_NAME, address=CONTRACT),
    ))
    print(f"\nCinchor Python smoke\n  contract: {CONTRACT_NAME} @ {CONTRACT}")
    print(f"  principal: {principal.address}\n  agent:     {agent.address}\n")

    print("1. mintCapability")
    minted = cinchor.mint_capability(principal=principal, agent=agent.address, max_spend=100, ttl_seconds=3600)
    cap = minted["capability_id"]
    state = cinchor.get_capability(cap)
    check("capability active after mint", state.status == CapabilityStatus.ACTIVE, state.status_label)
    check("maxSpend recorded", state.max_spend == 100, f"maxSpend={state.max_spend}")
    print(f"     capabilityId: {cap}")

    print("2. enforce 40 (in bounds)")
    a1 = cinchor.enforce(capability=cap, agent=agent, amount=40)
    check("first in-bounds action allowed", a1.allowed, a1.reason)

    print("3. enforce 40 (in bounds, total 80/100)")
    a2 = cinchor.enforce(capability=cap, agent=agent, amount=40)
    check("second in-bounds action allowed", a2.allowed, a2.reason)

    print("4. enforce 50 (over budget → refused)")
    a3 = cinchor.enforce(capability=cap, agent=agent, amount=50)
    check("over-budget action refused", not a3.allowed, a3.reason)
    check("refusal reason is over_budget", a3.code == EnforcementCode.OVER_BUDGET, a3.reason)
    after = cinchor.get_capability(cap)
    check("totalSpent = 80 (refusal did not mutate)", after.total_spent == 80, f"totalSpent={after.total_spent}")
    check("actionCount = 2", after.action_count == 2, f"actionCount={after.action_count}")

    print("5. attest a decision + verify (tamper-evidence)")
    decision = {"model": "demo-triage-v1", "input": {"claim": "A-123"}, "output": "approve", "at": now_secs()}
    att = cinchor.attest(capability=cap, agent=agent, context=decision, verdict=Verdict.IN_POLICY)
    good = cinchor.verify_attestation(decision, att["attestation_id"])
    check("attestation verifies against the original context", good["ok"], f"hash={att['context_hash'][:14]}…")
    tampered = cinchor.verify_attestation({**decision, "output": "deny"}, att["attestation_id"])
    check("tampered context fails verification", not tampered["ok"], "hash mismatch detected")

    print("6. revoke + enforce (refused: revoked)")
    cinchor.revoke(capability=cap, principal=principal)
    revoked = cinchor.get_capability(cap)
    check("capability revoked", revoked.status == CapabilityStatus.REVOKED, revoked.status_label)
    a4 = cinchor.enforce(capability=cap, agent=agent, amount=10)
    check("post-revocation action refused", not a4.allowed, a4.reason)
    check("refusal reason is revoked", a4.code == EnforcementCode.REVOKED, a4.reason)

    print(f"\n{'PASS' if failed == 0 else 'FAIL'} — {passed} checks passed, {failed} failed\n")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())

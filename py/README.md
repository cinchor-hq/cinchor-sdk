# Cinchor Python SDK (`cinchor`)

Accountability for autonomous agents on Omne — the capability + attestation
layer over the Omne base SDK ([`omne-sdk`](../../omne/sdk/python)). Two verbs:

- **`enforce(action)`** — authorize-or-refuse a consequential action. The
  substrate is the Policy Enforcement Point: an out-of-scope action commits no
  state change.
- **`attest(decision)`** — commit a tamper-evident, independently-verifiable
  record of a decision and its context.

Plus the capability lifecycle (mint / revoke / update / allow-counterparty) and
audit reads. Parity-matched with the TS `@cinchor/sdk`: capability ids,
counterparty keys, context hashes, and addresses derive identically across SDKs.

> Proprietary — © DoneUp, Inc. All rights reserved. Not open source.

## Install

Distributed via git (no public registry — Cinchor is proprietary). Both repos
are private, so this needs git access to `OmneDAO/omne-sdks` + `cinchor-hq/cinchor`.
Install the Omne base SDK first, then Cinchor:

```bash
# 1) Omne base SDK (dependency)
pip install "git+ssh://git@github.com/OmneDAO/omne-sdks.git@python/v0.1.0#subdirectory=python"
# 2) Cinchor
pip install "git+ssh://git@github.com/cinchor-hq/cinchor.git@sdk-py/v0.1.0#subdirectory=sdk-py"
```

(Use `git+https://…` instead of `git+ssh://…` if your git auth is HTTPS-based.)

Local development:
```bash
pip install -e ../../omne/sdk/python   # the Omne base SDK
pip install -e .                       # from sdk-py/
```

## Quickstart
```python
from omne_sdk import Wallet
from cinchor import CinchorClient, CinchorConfig, NetworkConfig, ContractConfig

principal = Wallet.from_mnemonic("…").get_account(0)
agent     = Wallet.from_mnemonic("…").get_account(0)

cinchor = CinchorClient.connect(CinchorConfig(
    network=NetworkConfig("ignis", 3, "http://127.0.0.1:26657"),
    contract=ContractConfig(name="cinchor_permissions", address="om1z…"),
))

cap = cinchor.mint_capability(principal=principal, agent=agent.address,
                              max_spend=100, ttl_seconds=3600)["capability_id"]
out = cinchor.enforce(capability=cap, agent=agent, amount=40)   # out.allowed / out.reason
att = cinchor.attest(capability=cap, agent=agent, context={"decision": "…"})
ok  = cinchor.verify_attestation({"decision": "…"}, att["attestation_id"])["ok"]
```

## Layout
| Module | Responsibility |
|---|---|
| `config.py` | network/contract config, `export_prefix_for`, `BURN_SENTINEL` |
| `types.py` | `CapabilityStatus` / `EnforcementCode` / `Verdict` + state dataclasses |
| `ids.py` | `derive_capability_id`, `counterparty_key` (om1z, on the base SDK) |
| `attestation.py` | canonical-JSON context hashing, `derive_attestation_id`, verify |
| `registry.py` | `CapabilityRegistry` — typed contract calls on `OmneClient` |
| `client.py` | `CinchorClient` — the two verbs + lifecycle + audit reads |

## Validate
`examples/full_flow.py` drives the full lifecycle against a live node:
`python full_flow.py <rpc> <contract> <wallets.json>` — mint → enforce (in-bounds
×2, over-budget refused) → attest + verify (tamper-evident) → revoke → refused.

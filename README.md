# @cinchor/sdk

**Accountability infrastructure for autonomous agents.** Bound what an agent can do *before* it acts, and prove what it did *after* — verifiably enough for a regulator, auditor, insurer, or court.

> Auth0 for auth · Datadog for observability · Vanta for compliance → **Cinchor for agent accountability.**

You `import` a library, wrap your agent's decision and action points, and call two verbs. You do **not** "go on a blockchain" — Cinchor runs on a neutral, independently-governed substrate that makes the records externally verifiable, but you never manage it.

```bash
npm install @cinchor/sdk
```

## Install — TypeScript, Python, Go

| Language | Package | Install |
|----------|---------|---------|
| TypeScript / JS | [`@cinchor/sdk`](https://www.npmjs.com/package/@cinchor/sdk) | `npm install @cinchor/sdk` |
| Python | [`cinchor`](https://pypi.org/project/cinchor/) | `pip install cinchor` |
| Go | `github.com/cinchor-hq/cinchor-sdk/go` | `go get github.com/cinchor-hq/cinchor-sdk/go` |

The TypeScript SDK (repo root + [`src/`](./src)) is the reference implementation. **Go** ([`go/`](./go)) and **Python** ([`py/`](./py)) are held to it by cross-SDK parity tests — identical capability-id and attestation-hash derivations, so a capability or record produced by one SDK is addressable and verifiable by another. All Apache-2.0.

## The two verbs

- **`enforce(action)`** — authorize-or-refuse a consequential action. The substrate is the enforcement point: an out-of-scope action commits **no state change**, no matter how the agent reasons, is prompted, or is compromised.
- **`attest(decision)`** — commit a tamper-evident, independently-verifiable record of a decision and the full context behind it, bound to the policy in force at the time.

Together they convert *unbounded* irreversible harm into *bounded* irreversible harm, with a record an adversary can verify.

## Quickstart

```ts
import { CinchorClient, IGNIS_LOCAL } from '@cinchor/sdk';
import { Wallet } from '@omne/sdk'; // or any compatible signer

const cinchor = await CinchorClient.connect({
  network: IGNIS_LOCAL,
  contract: { name: 'cinchor_permissions', address: 'om1z…' }, // your deployed contract
});

// 1. A principal mints a scoped capability to an agent.
const { capabilityId } = await cinchor.mintCapability({
  principal,                 // a Signer (the granting party)
  agent: agentAddress,       // om1z address of the agent
  maxSpend: 100n,            // spend ceiling
  ttlSeconds: 3600,          // expires in an hour
});

// 2. The agent enforces an action against that capability.
const outcome = await cinchor.enforce({
  capability: capabilityId,
  agent,                     // the agent Signer
  amount: 40n,
});
if (!outcome.allowed) {
  throw new Error(`action refused by the substrate: ${outcome.reason}`);
}

// 3. Attest a decision (provable-after).
const { attestationId } = await cinchor.attest({
  capability: capabilityId,
  agent,
  context: { model: 'claim-triage-v3', inputs, reasoning, output },
});

// 4. Anyone can verify, without trusting the operator.
const { ok } = await cinchor.verifyAttestation({ model: 'claim-triage-v3', inputs, reasoning, output }, attestationId);
```

## What `enforce` returns

`enforce()` returns an `EnforcementOutcome`:

| field | meaning |
|---|---|
| `allowed` | `true` only when the substrate authorized and recorded the action |
| `code` | `EnforcementCode` — `Allowed`, `NotFound`, `Revoked`, `Expired`, `OverBudget`, `OutOfAllowlist` |
| `reason` | the human-readable label for `code` |
| `receipt` | the on-chain commit receipt |

A `record_action` receipt does not carry the contract's return code, so the verdict is classified from committed state. Classification assumes serial, single-signer use of a capability (one in-flight action at a time) — the documented integration pattern.

## API surface

- **`CinchorClient.connect(config)`** — connect to a network + deployed contract.
- **Lifecycle:** `mintCapability`, `revoke`, `updatePolicy`, `allowCounterparty`.
- **Verbs:** `enforce`, `attest`.
- **Audit (reads, no signer):** `getCapability`, `getAttestation`, `verifyAttestation`.
- **Power users:** the underlying `CapabilityRegistry` is exposed at `client.registry`, and address/attestation utilities are exported directly.

## Signers

Any object exposing an om1z `address` and a `signTransaction(tx, opts)` method works (structurally typed as `Signer`). The Omne SDK's `Wallet` / `WalletAccount` satisfy it. The signer's address must be funded to pay gas.

## Substrate

Cinchor runs on Omne, an independently-governed L1 that supplies the neutral, verifiable settlement and audit layer. The chain is the substrate, not the product — the product is bounded, provable agent authority. This package depends on [`@omne/sdk`](https://www.npmjs.com/package/@omne/sdk) for signing and submission.

## License

Apache-2.0. Copyright © 2026 DoneUp, Inc. See [LICENSE](./LICENSE). The client
SDK is open source; the managed Cinchor gateway and service remain proprietary.

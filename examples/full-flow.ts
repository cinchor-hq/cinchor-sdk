/**
 * @cinchor/sdk — end-to-end example + smoke test.
 *
 * Proves the SDK against a live Omne node by driving the full lifecycle:
 *   mint capability → enforce (in-bounds, allowed) → enforce (over-budget, refused)
 *   → attest decision → verify (tamper-evident) → audit read → revoke → enforce (refused)
 *
 * It is both a runnable example and a smoke test: every step asserts the expected
 * outcome and the process exits non-zero on any mismatch.
 *
 * Run (against a local node with the contract deployed + wallets funded):
 *   CINCHOR_CONTRACT_ADDRESS=om1z… \
 *   CINCHOR_WALLETS=/path/to/wallets.json \
 *   npm run smoke
 *
 * Env:
 *   CINCHOR_RPC_URL          default http://127.0.0.1:26657
 *   CINCHOR_CHAIN_ID         default 3 (Ignis)
 *   CINCHOR_CONTRACT_NAME    default cinchor_permissions
 *   CINCHOR_CONTRACT_ADDRESS required — the deployed contract's om1z address
 *   CINCHOR_WALLETS          required — JSON: { principal:{mnemonic}, agent:{mnemonic} }
 */

import { readFileSync } from 'node:fs';
import { Wallet } from '@omne/sdk';
import {
  CinchorClient,
  EnforcementCode,
  CapabilityStatus,
  Verdict,
  nowSecs,
  type Signer,
} from '../dist/index.js';

// ── Tiny assertion harness ──────────────────────────────────────────
let passed = 0;
let failed = 0;
function check(label: string, cond: boolean, detail = ''): void {
  if (cond) {
    passed++;
    console.log(`  ✅ ${label}${detail ? `  (${detail})` : ''}`);
  } else {
    failed++;
    console.log(`  ❌ ${label}${detail ? `  (${detail})` : ''}`);
  }
}

// ── Config ──────────────────────────────────────────────────────────
function requireEnv(name: string): string {
  const v = process.env[name];
  if (!v) throw new Error(`${name} is required`);
  return v;
}

const RPC_URL = process.env.CINCHOR_RPC_URL ?? 'http://127.0.0.1:26657';
const CHAIN_ID = Number(process.env.CINCHOR_CHAIN_ID ?? 3);
const CONTRACT_NAME = process.env.CINCHOR_CONTRACT_NAME ?? 'cinchor_permissions';
const CONTRACT_ADDRESS = requireEnv('CINCHOR_CONTRACT_ADDRESS');
const WALLETS_PATH = requireEnv('CINCHOR_WALLETS');

function signerFromMnemonic(mnemonic: string): Signer {
  return Wallet.fromMnemonic(mnemonic).getAccount(0) as unknown as Signer;
}

async function main(): Promise<void> {
  const wallets = JSON.parse(readFileSync(WALLETS_PATH, 'utf8')) as {
    principal: { mnemonic: string };
    agent: { mnemonic: string };
  };
  const principal = signerFromMnemonic(wallets.principal.mnemonic);
  const agent = signerFromMnemonic(wallets.agent.mnemonic);

  console.log(`\nCinchor end-to-end smoke`);
  console.log(`  rpc:       ${RPC_URL} (chain ${CHAIN_ID})`);
  console.log(`  contract:  ${CONTRACT_NAME} @ ${CONTRACT_ADDRESS}`);
  console.log(`  principal: ${principal.address}`);
  console.log(`  agent:     ${agent.address}\n`);

  const cinchor = await CinchorClient.connect({
    network: { name: 'ignis', chainId: CHAIN_ID, rpcUrl: RPC_URL },
    contract: { name: CONTRACT_NAME, address: CONTRACT_ADDRESS },
  });

  // 1. Mint a scoped capability: ceiling 100, valid 1h, no allowlist.
  console.log('1. mintCapability (principal grants agent a capability)');
  const { capabilityId } = await cinchor.mintCapability({
    principal,
    agent: agent.address,
    maxSpend: 100n,
    ttlSeconds: 3600,
  });
  const minted = await cinchor.getCapability(capabilityId);
  check('capability is active after mint', minted.status === CapabilityStatus.Active, minted.statusLabel);
  check('maxSpend recorded', minted.maxSpend === 100n, `maxSpend=${minted.maxSpend}`);
  console.log(`     capabilityId: ${capabilityId}`);

  // 2. In-bounds action → allowed.
  console.log('2. enforce 40 (in bounds)');
  const a1 = await cinchor.enforce({ capability: capabilityId, agent, amount: 40n });
  check('first in-bounds action allowed', a1.allowed, a1.reason);

  // 3. Second in-bounds action → allowed (total 80/100).
  console.log('3. enforce 40 (in bounds, total 80/100)');
  const a2 = await cinchor.enforce({ capability: capabilityId, agent, amount: 40n });
  check('second in-bounds action allowed', a2.allowed, a2.reason);

  // 4. Over-budget action → refused by the substrate (80 + 50 > 100).
  console.log('4. enforce 50 (over budget → must be refused)');
  const a3 = await cinchor.enforce({ capability: capabilityId, agent, amount: 50n });
  check('over-budget action refused', !a3.allowed, a3.reason);
  check('refusal reason is over_budget', a3.code === EnforcementCode.OverBudget, a3.reason);

  // 5. State reflects exactly the two committed actions.
  const afterActions = await cinchor.getCapability(capabilityId);
  check('totalSpent = 80 (refusal did not mutate)', afterActions.totalSpent === 80n, `totalSpent=${afterActions.totalSpent}`);
  check('actionCount = 2', afterActions.actionCount === 2n, `actionCount=${afterActions.actionCount}`);

  // 6. Attest a decision; verify it is tamper-evident.
  console.log('5. attest a decision + verify (tamper-evidence)');
  const decision = { model: 'demo-triage-v1', input: { claim: 'A-123' }, output: 'approve', at: Number(nowSecs()) };
  const { attestationId, contextHash } = await cinchor.attest({
    capability: capabilityId,
    agent,
    context: decision,
    verdict: Verdict.InPolicy,
  });
  const good = await cinchor.verifyAttestation(decision, attestationId);
  check('attestation verifies against the original context', good.ok, `hash=${contextHash.slice(0, 14)}…`);
  const tampered = await cinchor.verifyAttestation({ ...decision, output: 'deny' }, attestationId);
  check('tampered context fails verification', !tampered.ok, 'hash mismatch detected');

  // 7. Revoke, then prove the substrate refuses post-revocation actions.
  console.log('6. revoke + enforce (must be refused: revoked)');
  await cinchor.revoke({ capability: capabilityId, principal });
  const revokedState = await cinchor.getCapability(capabilityId);
  check('capability is revoked', revokedState.status === CapabilityStatus.Revoked, revokedState.statusLabel);
  const a4 = await cinchor.enforce({ capability: capabilityId, agent, amount: 1n });
  check('post-revocation action refused', !a4.allowed, a4.reason);
  check('refusal reason is revoked', a4.code === EnforcementCode.Revoked, a4.reason);

  // ── Summary ────────────────────────────────────────────────────
  console.log(`\n${failed === 0 ? '✅ PASS' : '❌ FAIL'} — ${passed} checks passed, ${failed} failed\n`);
  if (failed > 0) process.exit(1);
}

main().catch((err) => {
  console.error('\n❌ smoke run errored:', err);
  process.exit(1);
});

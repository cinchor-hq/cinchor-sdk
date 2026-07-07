/**
 * @cinchor/sdk — decision attestation (provable-after).
 *
 * A decision attestation commits, on-chain, the HASH of a decision's full
 * context (inputs the agent saw, policy version, model, reasoning, output) plus
 * a verdict — bound to the policy version in force at decision time. The full
 * artifact lives off-chain; the tamper-evidence property is that anyone can
 * re-hash the off-chain artifact and confirm it matches the on-chain
 * context_hash. If a byte changed, the hashes diverge and tampering is detected.
 *
 * This is what makes non-payment decisions (claims denials, underwriting calls)
 * accountable: provable after the fact to a party who does not trust the operator.
 */

import { encodeAddress, decodeAddress, sha256, u64be, concat } from './address.js';

/**
 * Deterministic JSON: object keys sorted recursively so the same logical
 * context always serializes to the same bytes (stable hashing across machines).
 */
export function canonicalJson(value: unknown): string {
  const norm = (v: unknown): unknown => {
    if (Array.isArray(v)) return v.map(norm);
    if (v && typeof v === 'object') {
      return Object.keys(v as Record<string, unknown>)
        .sort()
        .reduce((acc: Record<string, unknown>, k) => {
          acc[k] = norm((v as Record<string, unknown>)[k]);
          return acc;
        }, {});
    }
    return v;
  };
  return JSON.stringify(norm(value));
}

/** Hash a decision-context object to a 32-byte om1z commitment. */
export async function hashDecisionContext(context: unknown): Promise<string> {
  const bytes = new TextEncoder().encode(canonicalJson(context));
  return encodeAddress(await sha256(bytes));
}

/**
 * Derive a deterministic attestation id from (capabilityId, contextHash, seq).
 * The contract enforces first-write, so reuse fails. Returns a 32-byte om1z id.
 */
export async function deriveAttestationId(
  capabilityId: string,
  contextHash: string,
  seq: number,
): Promise<string> {
  const preimage = concat(
    decodeAddress(capabilityId),
    decodeAddress(contextHash),
    u64be(seq),
  );
  return encodeAddress(await sha256(preimage));
}

/**
 * Tamper check: re-hash a decision context and confirm it matches the on-chain
 * commitment. Returns whether it matches and the recomputed hash.
 */
export async function verifyDecisionContext(
  context: unknown,
  onChainContextHash: string,
): Promise<{ ok: boolean; recomputed: string }> {
  const recomputed = await hashDecisionContext(context);
  return { ok: recomputed === onChainContextHash, recomputed };
}

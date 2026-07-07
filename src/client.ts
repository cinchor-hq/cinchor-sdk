/**
 * @cinchor/sdk — the high-level developer facade.
 *
 * Two verbs carry the product:
 *   - enforce(action)  — authorize-or-refuse a consequential action. The
 *                        substrate is the Policy Enforcement Point: an
 *                        out-of-scope action commits no state change.
 *   - attest(decision) — commit a tamper-evident, independently-verifiable
 *                        record of a decision and its context.
 *
 * Plus the capability lifecycle (mint / revoke / update / allow-counterparty)
 * and the audit reads a relying party uses to verify without trusting you.
 *
 * The chain is the substrate, not the product: callers `import` this, wrap
 * their agent's decision points, and never manage a blockchain.
 */

import { CapabilityRegistry } from './capability-registry.js';
import { hashDecisionContext, deriveAttestationId, verifyDecisionContext } from './attestation.js';
import { deriveCapabilityId } from './address.js';
import { type CinchorConfig, BURN_SENTINEL } from './config.js';
import {
  CapabilityStatus,
  EnforcementCode,
  ENFORCEMENT_LABELS,
  Verdict,
  type Signer,
  type SubmitReceipt,
  type EnforcementOutcome,
  type CapabilityState,
  type AttestationRecord,
} from './types.js';

/** Current UNIX time in seconds, as the bigint the contract expects. */
export function nowSecs(): bigint {
  return BigInt(Math.floor(Date.now() / 1000));
}

/**
 * Poll `read` until `done` holds or the timeout elapses, returning the last
 * value read. A committed transaction's receipt can resolve before the
 * committed state is consistently queryable on a multi-validator mesh; settling
 * makes read-after-write deterministic for the caller.
 */
async function settle<T>(
  read: () => Promise<T>,
  done: (v: T) => boolean,
  timeoutMs = 12_000,
  intervalMs = 750,
): Promise<T> {
  const start = Date.now();
  let last = await read();
  while (!done(last) && Date.now() - start < timeoutMs) {
    await new Promise((r) => setTimeout(r, intervalMs));
    last = await read();
  }
  return last;
}

export interface MintCapabilityOptions {
  /** The principal granting authority. Signs the mint. */
  principal: Signer;
  /** om1z address of the agent being granted scoped authority. */
  agent: string;
  /** Spend ceiling (in the unit the action amounts are denominated in). */
  maxSpend: bigint;
  /** Absolute expiry (UNIX seconds). Provide this OR `ttlSeconds`. */
  validUntil?: bigint;
  /** Relative expiry from now (seconds). Used when `validUntil` is omitted. */
  ttlSeconds?: number;
  /** Enforce a counterparty allowlist on every action under this capability. */
  allowlist?: boolean;
  /** Uniqueness nonce for the derived id. Defaults to a random value. */
  nonce?: number;
  /** Override the recorded creation time (UNIX seconds). Defaults to now. */
  currentTime?: bigint;
  gasLimit?: number;
}

export interface MintCapabilityResult {
  capabilityId: string;
  receipt: SubmitReceipt;
}

export interface EnforceOptions {
  /** The capability id authorizing this action. */
  capability: string;
  /** The agent performing the action. Signs the record. */
  agent: Signer;
  /** Amount this action spends against the capability's ceiling. */
  amount: bigint;
  /** om1z counterparty the action transacts with (required if allowlisted). */
  counterparty?: string;
  /** Override the action time (UNIX seconds). Defaults to now. */
  currentTime?: bigint;
  gasLimit?: number;
}

export interface AttestOptions {
  /** The capability the decision was made under (binds the policy version). */
  capability: string;
  /** The agent attesting. Signs the attestation. */
  agent: Signer;
  /** The full decision context. Hashed canonically; the hash is committed. */
  context: unknown;
  /** Verdict committed alongside the context. Defaults to in-policy. */
  verdict?: Verdict;
  /** Sequence number disambiguating multiple attestations. Defaults to 0. */
  seq?: number;
  currentTime?: bigint;
  gasLimit?: number;
}

export interface AttestResult {
  attestationId: string;
  contextHash: string;
  verdict: Verdict;
  receipt: SubmitReceipt;
}

export class CinchorClient {
  readonly registry: CapabilityRegistry;

  private constructor(registry: CapabilityRegistry) {
    this.registry = registry;
  }

  /** Connect to a network + deployed accountability contract. */
  static async connect(config: CinchorConfig): Promise<CinchorClient> {
    return new CinchorClient(await CapabilityRegistry.connect(config));
  }

  // ── Capability lifecycle ─────────────────────────────────────────

  /**
   * Mint a cryptographically-scoped capability to an agent: capability, spend
   * ceiling, validity window, revocable at will. Returns the derived capability
   * id and the commit receipt.
   */
  async mintCapability(opts: MintCapabilityOptions): Promise<MintCapabilityResult> {
    const currentTime = opts.currentTime ?? nowSecs();
    const validUntil =
      opts.validUntil ??
      (opts.ttlSeconds !== undefined
        ? currentTime + BigInt(opts.ttlSeconds)
        : (() => {
            throw new Error('mintCapability requires either validUntil or ttlSeconds');
          })());
    const nonce = opts.nonce ?? Math.floor(Math.random() * 2 ** 48);
    const capabilityId = await deriveCapabilityId(
      opts.principal.address,
      opts.agent,
      nonce,
      Number(currentTime),
    );
    const receipt = await this.registry.mintPermission({
      signer: opts.principal,
      capabilityId,
      principal: opts.principal.address,
      agent: opts.agent,
      maxSpend: opts.maxSpend,
      validUntil,
      allowlistEnabled: opts.allowlist,
      currentTime,
      gasLimit: opts.gasLimit,
    });
    await settle(
      () => this.getCapability(capabilityId),
      (c) => c.status === CapabilityStatus.Active,
    );
    return { capabilityId, receipt };
  }

  /** Revoke a capability. Terminal. Signed by the principal. */
  async revoke(opts: {
    capability: string;
    principal: Signer;
    currentTime?: bigint;
    gasLimit?: number;
  }): Promise<SubmitReceipt> {
    const receipt = await this.registry.revokePermission({
      signer: opts.principal,
      capabilityId: opts.capability,
      currentTime: opts.currentTime ?? nowSecs(),
      gasLimit: opts.gasLimit,
    });
    await settle(
      () => this.getCapability(opts.capability),
      (c) => c.status === CapabilityStatus.Revoked,
    );
    return receipt;
  }

  /** Update a capability's policy (limits) and bump its on-chain version. */
  updatePolicy(opts: {
    capability: string;
    principal: Signer;
    maxSpend: bigint;
    validUntil: bigint;
    currentTime?: bigint;
    gasLimit?: number;
  }): Promise<SubmitReceipt> {
    return this.registry.updatePolicy({
      signer: opts.principal,
      capabilityId: opts.capability,
      newMaxSpend: opts.maxSpend,
      newValidUntil: opts.validUntil,
      currentTime: opts.currentTime ?? nowSecs(),
      gasLimit: opts.gasLimit,
    });
  }

  /** Authorize a counterparty for a capability's allowlist. */
  allowCounterparty(opts: {
    capability: string;
    principal: Signer;
    counterparty: string;
    currentTime?: bigint;
    gasLimit?: number;
  }): Promise<SubmitReceipt> {
    return this.registry.addAllowedCounterparty({
      signer: opts.principal,
      capabilityId: opts.capability,
      counterparty: opts.counterparty,
      currentTime: opts.currentTime ?? nowSecs(),
      gasLimit: opts.gasLimit,
    });
  }

  // ── The two verbs ────────────────────────────────────────────────

  /**
   * Authorize-or-refuse a consequential action. The substrate enforces the
   * capability's invariants atomically: an out-of-scope action commits no state
   * change. Deny outcomes are decided from the pre-state (spend only grows,
   * expiry and revocation are monotonic), so a refusal returns immediately —
   * no doomed transaction, no gas, no settle wait. The contract remains the
   * backstop: a stale pre-state read can only under-report headroom, never
   * grant it. An allowed verdict is read back from committed state (a
   * record_action receipt does not carry the contract's return code); if the
   * commit is not observable within the settle window this throws rather than
   * fabricating a deny, because the transaction may still land.
   */
  async enforce(opts: EnforceOptions): Promise<EnforcementOutcome> {
    const currentTime = opts.currentTime ?? nowSecs();
    const before = await this.registry.getCapabilityState(opts.capability);
    // Deny codes backed by POSITIVE evidence (a committed capability observed
    // revoked / expired / over budget / allowlist-refusing) fast-deny from the
    // pre-state. ABSENCE is not evidence: a NotFound read may be a freshly
    // minted capability not yet visible (state reads are not read-your-writes),
    // so it falls through to the submit-and-settle path, which absorbs the
    // visibility lag exactly as it always did.
    const code = await this.prejudge(before, opts.amount, currentTime, opts);
    if (code !== EnforcementCode.Allowed && before.status !== CapabilityStatus.NotFound) {
      return { allowed: false, code, reason: ENFORCEMENT_LABELS[code] };
    }
    const receipt = await this.registry.recordAction({
      signer: opts.agent,
      capabilityId: opts.capability,
      amountSpent: opts.amount,
      counterparty: opts.counterparty,
      currentTime,
      gasLimit: opts.gasLimit,
    });
    // Settle the after-read: an allowed action increments actionCount once the
    // commit is observable.
    const after = await settle(
      () => this.registry.getCapabilityState(opts.capability),
      (a) => a.actionCount > before.actionCount,
      8_000,
    );
    if (after.actionCount > before.actionCount && after.totalSpent === before.totalSpent + opts.amount) {
      return {
        allowed: true,
        code: EnforcementCode.Allowed,
        reason: ENFORCEMENT_LABELS[EnforcementCode.Allowed],
        receipt,
      };
    }
    if (before.status === CapabilityStatus.NotFound) {
      // The capability was not visible pre-submit and no commit was observed.
      // Re-judge from the freshest state: if it appeared, report its real
      // refusal; if it still doesn't exist, not_found is now earned.
      const late = await this.prejudge(after, opts.amount, currentTime, opts);
      if (late !== EnforcementCode.Allowed) {
        return { allowed: false, code: late, reason: ENFORCEMENT_LABELS[late] };
      }
    }
    // The capability allows the action but the commit wasn't observable within
    // the settle window (slow block or a concurrent writer on this capability).
    // The transaction may still land — never fabricate a deny for it.
    throw new Error(
      `enforce unsettled: action on ${opts.capability} not confirmed within settle window (tx ${receipt.transactionHash})`,
    );
  }

  /**
   * Evaluate the deny codes against the pre-state, in the contract's
   * precedence order. Allowed here means "nothing refuses it pre-submit".
   */
  private async prejudge(
    before: CapabilityState,
    amount: bigint,
    t: bigint,
    opts: EnforceOptions,
  ): Promise<EnforcementCode> {
    if (before.status === CapabilityStatus.NotFound) return EnforcementCode.NotFound;
    if (before.status === CapabilityStatus.Revoked) return EnforcementCode.Revoked;
    if (t > before.validUntil) return EnforcementCode.Expired;
    if (before.totalSpent + amount > before.maxSpend) return EnforcementCode.OverBudget;
    if (await this.registry.getAllowlistEnabled(opts.capability)) {
      const cp = opts.counterparty ?? BURN_SENTINEL;
      if (!(await this.registry.isCounterpartyAllowed(opts.capability, cp))) {
        return EnforcementCode.OutOfAllowlist;
      }
    }
    return EnforcementCode.Allowed;
  }

  /**
   * Commit a tamper-evident attestation of a decision. The full context is
   * hashed canonically; the hash + verdict are committed on-chain, bound to the
   * capability's current policy version. An auditor later re-hashes the
   * off-chain artifact and confirms it matches (see {@link verifyAttestation}).
   */
  async attest(opts: AttestOptions): Promise<AttestResult> {
    const currentTime = opts.currentTime ?? nowSecs();
    const verdict = opts.verdict ?? Verdict.InPolicy;
    const seq = opts.seq ?? 0;
    const contextHash = await hashDecisionContext(opts.context);
    const attestationId = await deriveAttestationId(opts.capability, contextHash, seq);
    const receipt = await this.registry.recordAttestation({
      signer: opts.agent,
      attestationId,
      capabilityId: opts.capability,
      contextHash,
      verdict,
      currentTime,
      gasLimit: opts.gasLimit,
    });
    await settle(() => this.registry.getAttestation(attestationId), (a) => a.exists);
    return { attestationId, contextHash, verdict, receipt };
  }

  // ── Audit (reads; no signer required) ────────────────────────────

  /** Read the full on-chain state of a capability. */
  getCapability(capabilityId: string): Promise<CapabilityState> {
    return this.registry.getCapabilityState(capabilityId);
  }

  /** Read a decision attestation's on-chain record. */
  getAttestation(attestationId: string): Promise<AttestationRecord> {
    return this.registry.getAttestation(attestationId);
  }

  /**
   * Tamper check: re-hash a decision context and confirm it matches the
   * attestation's on-chain commitment. Returns whether it matches, the
   * recomputed hash, and the on-chain record.
   */
  async verifyAttestation(
    context: unknown,
    attestationId: string,
  ): Promise<{ ok: boolean; recomputed: string; onChain: AttestationRecord }> {
    const onChain = await this.registry.getAttestation(attestationId);
    const { ok, recomputed } = await verifyDecisionContext(context, onChain.contextHash);
    return { ok: ok && onChain.exists, recomputed, onChain };
  }
}

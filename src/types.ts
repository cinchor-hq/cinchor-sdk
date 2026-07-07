/**
 * @cinchor/sdk — shared types and on-chain outcome codes.
 *
 * These mirror the typed return codes the on-chain accountability contract
 * (the substrate Policy Enforcement Point) emits. They are the contract of the
 * substrate, surfaced to the developer as named outcomes rather than integers.
 */

/** Lifecycle state of a capability (permission token). */
export enum CapabilityStatus {
  NotFound = 0,
  Active = 1,
  Revoked = 2,
}

export type CapabilityStatusLabel = 'not_found' | 'active' | 'revoked';

export const CAPABILITY_STATUS_LABELS: readonly CapabilityStatusLabel[] = [
  'not_found',
  'active',
  'revoked',
] as const;

/**
 * Result of an `enforce()` call — the substrate's authorize-or-refuse verdict
 * for an action recorded under a capability. Code 1 is the only allow; every
 * other code is a substrate-enforced refusal (no state mutation occurred).
 */
export enum EnforcementCode {
  NotFound = 0,
  Allowed = 1,
  Revoked = 2,
  Expired = 3,
  OverBudget = 4,
  OutOfAllowlist = 5,
  /**
   * The caller is not the token's designated agent (on-chain caller binding).
   * Legitimate callers sign with the agent key and never see it; it surfaces
   * only on a forged/misdirected call, and is a write-free refusal on-chain.
   */
  Unauthorized = 6,
}

export type EnforcementLabel =
  | 'not_found'
  | 'allowed'
  | 'revoked'
  | 'expired'
  | 'over_budget'
  | 'out_of_allowlist'
  | 'unauthorized';

export const ENFORCEMENT_LABELS: Record<EnforcementCode, EnforcementLabel> = {
  [EnforcementCode.NotFound]: 'not_found',
  [EnforcementCode.Allowed]: 'allowed',
  [EnforcementCode.Revoked]: 'revoked',
  [EnforcementCode.Expired]: 'expired',
  [EnforcementCode.OverBudget]: 'over_budget',
  [EnforcementCode.OutOfAllowlist]: 'out_of_allowlist',
  [EnforcementCode.Unauthorized]: 'unauthorized',
};

/** Verdict committed alongside a decision attestation. */
export enum Verdict {
  InPolicy = 1,
  OutOfPolicy = 2,
}

/** A signer compatible with the Omne SDK's WalletAccount.
 *
 * Defined structurally so callers can pass any object exposing an om1z
 * `address` and a `signTransaction` method — they need not import the Omne
 * SDK's concrete `WalletAccount` type to use Cinchor.
 */
export interface Signer {
  readonly address: string;
  signTransaction(
    tx: Record<string, unknown>,
    opts?: { chainId?: number },
  ): {
    from: string;
    to: string;
    value: string;
    gasLimit: number;
    gasPrice: string;
    nonce: number;
    chainId: number;
    data?: string;
    signature: string;
    publicKey: string;
  };
}

/** Receipt-shaped result returned from a committed (or pending) write. */
export interface SubmitReceipt {
  transactionHash: string | null;
  status: string;
  blockNumber: number | null;
  note?: string;
}

/** The outcome of an `enforce()` call. */
export interface EnforcementOutcome {
  /** True only when the substrate authorized the action (code 1). */
  allowed: boolean;
  code: EnforcementCode;
  reason: EnforcementLabel;
  /** Present only when a transaction was submitted (i.e. not a pre-state deny). */
  receipt?: SubmitReceipt;
}

/** Full on-chain state of a capability. */
export interface CapabilityState {
  capabilityId: string;
  status: CapabilityStatus;
  statusLabel: CapabilityStatusLabel;
  principal: string;
  agent: string;
  maxSpend: bigint;
  validUntil: bigint;
  totalSpent: bigint;
  actionCount: bigint;
  createdAt: bigint;
  revokedAt: bigint;
}

/** On-chain record of a decision attestation (the tamper-evidence anchor). */
export interface AttestationRecord {
  exists: boolean;
  contextHash: string;
  policyVersion: bigint;
  verdict: number;
  time: bigint;
}

/**
 * @cinchor/sdk — typed wrapper over the deployed accountability contract.
 *
 * Each method maps to a contract export, encoding arguments per the on-chain
 * ABI and parsing the returned state. This is the low-level primitive layer;
 * most consumers use the high-level {@link CinchorClient} facade instead.
 *
 * Read methods (query) work standalone — no wallet/signing required.
 * Write methods (call) require a signer whose om1z address is funded to pay gas.
 *
 * Substrate-enforced invariants for an action (in precedence order):
 *   0 = not_found, 1 = success, 2 = revoked, 3 = expired, 4 = over_budget,
 *   5 = out_of_allowlist.
 */

import {
  OmneClient,
  OmneContract,
  AbiEncode,
  encodeContractCall,
  type AbiArgument,
  type ContractQueryResult,
} from '@omne/sdk';
import { addressArg, counterpartyKey, encodeAddress } from './address.js';
import {
  type CinchorConfig,
  type ContractConfig,
  exportPrefixFor,
  BURN_SENTINEL,
  DEFAULT_GAS_LIMIT,
  DEFAULT_GAS_PRICE,
} from './config.js';
import {
  CapabilityStatus,
  CAPABILITY_STATUS_LABELS,
  type CapabilityState,
  type AttestationRecord,
  type Signer,
  type SubmitReceipt,
} from './types.js';

/** A JSON-RPC submit result, discriminated so a real rejection is never swallowed. */
type SubmitStatus = 'ok' | 'duplicate' | 'stale_nonce';

function toBigInt(v: ContractQueryResult['returnValue']): bigint {
  return BigInt(v ?? 0);
}

function toNumber(v: ContractQueryResult['returnValue']): number {
  return Number(v ?? 0);
}

export class CapabilityRegistry {
  private readonly contract: OmneContract;
  private readonly rpcUrl: string;
  private readonly chainId: number;
  private readonly contractAddress: string;
  private readonly exportPrefix: string;
  private readonly defaultGasLimit: number;
  private readonly defaultGasPrice: string;
  /** Per-signer next-nonce cache (see {@link sendSigned} for node nonce semantics). */
  private readonly nonces = new Map<string, number>();

  private constructor(contract: OmneContract, config: CinchorConfig) {
    this.contract = contract;
    this.rpcUrl = config.network.rpcUrl;
    this.chainId = config.network.chainId;
    this.contractAddress = config.contract.address;
    this.exportPrefix = exportPrefixFor(config.contract);
    this.defaultGasLimit = config.defaultGasLimit ?? DEFAULT_GAS_LIMIT;
    this.defaultGasPrice = config.defaultGasPrice ?? DEFAULT_GAS_PRICE;
  }

  static async connect(config: CinchorConfig): Promise<CapabilityRegistry> {
    const client = new OmneClient(config.network.rpcUrl);
    await client.connect();
    const contract = new OmneContract(client, config.contract.address);
    return new CapabilityRegistry(contract, config);
  }

  private fn(name: string): string {
    return this.exportPrefix + name;
  }

  /** A single, non-retrying JSON-RPC POST. */
  private async rpc<T = unknown>(method: string, params: unknown[]): Promise<T> {
    const resp = await fetch(this.rpcUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ jsonrpc: '2.0', method, params, id: 1 }),
    });
    if (!resp.ok) {
      throw new Error(`${method} HTTP ${resp.status}: ${await resp.text()}`);
    }
    const json = (await resp.json()) as { result?: T; error?: { message?: string } };
    if (json.error) throw new Error(`${method} rejected: ${json.error.message ?? 'unknown'}`);
    return json.result as T;
  }

  // ── Write path ──────────────────────────────────────────────────

  /**
   * Build, SIGN, and submit a state-changing contract call. The Omne node
   * mandates a valid ML-DSA-44 signature on every transaction; we encode the
   * call data, build the transaction, sign it with the role's signer (chainId
   * 3 = Ignis), and submit via a single raw JSON-RPC POST. The submitter
   * ("from") must equal the signer's om1z address — the node re-derives the PQC
   * address from the public key and checks it matches.
   */
  private async sendSigned(
    signer: Signer,
    method: string,
    args: AbiArgument[],
    gasLimit = this.defaultGasLimit,
  ): Promise<SubmitReceipt> {
    const data = encodeContractCall(this.fn(method), args);

    // Node nonce semantics: this devnet node does NOT currently track or enforce
    // per-account nonces — omne_getNonce / omne_getAccount report 0 even for a
    // signer with many committed txs, and any nonce is accepted. We keep a
    // client-side monotonic counter (seeded once from the node) for hygiene.
    // If/when the node enforces real nonces, the seed becomes authoritative and
    // the stale-nonce recovery below takes over.
    let nonce = this.nonces.get(signer.address);
    if (nonce === undefined) nonce = await this.fetchNonce(signer.address);

    // Build → sign → submit, retrying ONCE on "nonce too low" (a stale seed).
    // The tx is signed WITH the nonce, so recovery re-signs with a fresh one.
    // "nonce too low" is a definitive rejection (tx NOT accepted), so re-signing
    // higher cannot double-submit. "duplicate" means the node already holds this
    // exact tx — treated as accepted; outcome is read from state.
    for (let attempt = 0; attempt < 2; attempt++) {
      const wire = this.buildSignedWire(signer, data, nonce, gasLimit);
      const res = await this.submitOnce(wire);
      if (res.status === 'ok' || res.status === 'duplicate') {
        this.nonces.set(signer.address, nonce + 1);
        return this.pollReceipt(res.txHash);
      }
      const fresh = await this.fetchNonce(signer.address);
      if (attempt === 0 && fresh > nonce) {
        nonce = fresh;
        this.nonces.set(signer.address, fresh);
        continue;
      }
      throw new Error(
        `transaction rejected nonce ${nonce} as too low; node reports ${fresh} (cannot recover)`,
      );
    }
    throw new Error('sendSigned: exhausted submit attempts'); // unreachable
  }

  /** Fetch the signer's next nonce from the node (0 on this devnet). */
  private async fetchNonce(address: string): Promise<number> {
    try {
      const n = await this.rpc<unknown>('omne_getNonce', [address]);
      return Number(n ?? 0);
    } catch {
      const acct = await this.rpc<{ nonce?: number }>('omne_getAccount', [address]);
      return Number(acct?.nonce ?? 0);
    }
  }

  /**
   * Build a signed wire payload for a contract call at a given nonce, validating
   * the ML-DSA-44 signature/pubkey/chainId lengths locally before submit so a
   * malformed signed object fails fast here, not at the node.
   */
  private buildSignedWire(
    signer: Signer,
    data: string,
    nonce: number,
    gasLimit: number,
  ): Record<string, unknown> {
    const tx = {
      from: signer.address,
      to: this.contractAddress,
      value: '0',
      gasLimit,
      gasPrice: this.defaultGasPrice,
      nonce,
      data,
      chainId: this.chainId,
    };
    const signed = signer.signTransaction(tx, { chainId: this.chainId });
    // ML-DSA-44: 2420-byte signature (4840 hex), 1312-byte public key (2624 hex).
    if (typeof signed.signature !== 'string' || !/^[0-9a-f]{4840}$/i.test(signed.signature)) {
      throw new Error(
        `signTransaction produced an invalid ML-DSA-44 signature (expected 4840 hex chars, got ${signed.signature?.length ?? 0})`,
      );
    }
    if (typeof signed.publicKey !== 'string' || !/^[0-9a-f]{2624}$/i.test(signed.publicKey)) {
      throw new Error(
        `signTransaction produced an invalid ML-DSA-44 public key (expected 2624 hex chars, got ${signed.publicKey?.length ?? 0})`,
      );
    }
    if (!Number.isInteger(signed.chainId) || signed.chainId < 0) {
      throw new Error(`signed tx carries an invalid chainId: ${signed.chainId}`);
    }
    return {
      from: signed.from,
      to: signed.to,
      value: signed.value,
      gasLimit: signed.gasLimit,
      gasPrice: signed.gasPrice,
      nonce: signed.nonce,
      chainId: signed.chainId,
      data: signed.data ?? '',
      signature: { signature: signed.signature, publicKey: signed.publicKey },
    };
  }

  /** Submit a signed wire payload with one non-retrying POST. */
  private async submitOnce(
    wire: Record<string, unknown>,
  ): Promise<{ status: SubmitStatus; txHash: string | null }> {
    const resp = await fetch(this.rpcUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ jsonrpc: '2.0', method: 'omne_sendTransaction', params: [wire], id: 1 }),
    });
    if (!resp.ok) {
      throw new Error(`omne_sendTransaction HTTP ${resp.status}: ${await resp.text()}`);
    }
    const json = (await resp.json()) as {
      result?: string | { transactionHash?: string };
      error?: { message?: string };
    };
    if (json.error) {
      const msg = String(json.error.message ?? json.error);
      if (/already\s*known|already\s*exists|duplicate/i.test(msg)) {
        return { status: 'duplicate', txHash: null };
      }
      if (/nonce\s*too\s*low|invalid\s*nonce/i.test(msg)) {
        return { status: 'stale_nonce', txHash: null };
      }
      throw new Error(`omne_sendTransaction rejected: ${msg}`);
    }
    const r = json.result;
    return { status: 'ok', txHash: typeof r === 'string' ? r : (r?.transactionHash ?? null) };
  }

  /**
   * Poll for a transaction receipt, tolerating the node's "receipt not found"
   * RPC error while the tx is still in the mempool. 60s default: a 4-node BFT
   * mesh confirms contract calls in ~20-30s.
   */
  private async pollReceipt(
    txHash: string | null,
    timeoutMs = 60_000,
    intervalMs = 1_000,
  ): Promise<SubmitReceipt> {
    if (!txHash) return { transactionHash: null, status: 'submitted', blockNumber: null };
    const start = Date.now();
    let lastTransportError: string | null = null;
    while (Date.now() - start < timeoutMs) {
      try {
        const r = await this.rpc<SubmitReceipt | null>('omne_getTransactionReceipt', [txHash]);
        if (r) return r;
      } catch (e: unknown) {
        const msg = e instanceof Error ? e.message : String(e);
        if (/receipt.*not\s*found|not\s*found|-32000|-32602/i.test(msg)) {
          // Expected: tx still in the mempool — keep polling quietly.
        } else {
          lastTransportError = msg;
        }
      }
      await new Promise((res) => setTimeout(res, intervalMs));
    }
    return {
      transactionHash: txHash,
      status: 'pending',
      blockNumber: null,
      note: lastTransportError
        ? `receipt not available within ${timeoutMs}ms; last transport error: ${lastTransportError}`
        : `receipt not available within ${timeoutMs}ms`,
    };
  }

  // ── Reads ───────────────────────────────────────────────────────

  private async queryNumber(method: string, id: string): Promise<number> {
    const r = await this.contract.query(this.fn(method), [addressArg(id)]);
    return toNumber(r.returnValue);
  }

  private async queryBigInt(method: string, id: string): Promise<bigint> {
    const r = await this.contract.query(this.fn(method), [addressArg(id)]);
    return toBigInt(r.returnValue);
  }

  private async queryAddress(method: string, id: string): Promise<string> {
    const r = await this.contract.query(this.fn(method), [addressArg(id)]);
    return parseAddressReturn(r.returnValue);
  }

  getStatus(capabilityId: string): Promise<number> {
    return this.queryNumber('get_status', capabilityId);
  }
  getMaxSpend(capabilityId: string): Promise<bigint> {
    return this.queryBigInt('get_max_spend', capabilityId);
  }
  getValidUntil(capabilityId: string): Promise<bigint> {
    return this.queryBigInt('get_valid_until', capabilityId);
  }
  getTotalSpent(capabilityId: string): Promise<bigint> {
    return this.queryBigInt('get_total_spent', capabilityId);
  }
  getActionCount(capabilityId: string): Promise<bigint> {
    return this.queryBigInt('get_action_count', capabilityId);
  }
  getCreatedAt(capabilityId: string): Promise<bigint> {
    return this.queryBigInt('get_created_at', capabilityId);
  }
  getRevokedAt(capabilityId: string): Promise<bigint> {
    return this.queryBigInt('get_revoked_at', capabilityId);
  }
  getPolicyVersion(capabilityId: string): Promise<bigint> {
    return this.queryBigInt('get_policy_version', capabilityId);
  }
  getAttestationCount(capabilityId: string): Promise<bigint> {
    return this.queryBigInt('get_attestation_count', capabilityId);
  }

  async getAllowlistEnabled(capabilityId: string): Promise<boolean> {
    return (await this.queryNumber('get_allowlist_enabled', capabilityId)) === 1;
  }

  async isCounterpartyAllowed(capabilityId: string, counterparty: string): Promise<boolean> {
    const key = await counterpartyKey(capabilityId, counterparty);
    const r = await this.contract.query(this.fn('is_counterparty_allowed'), [addressArg(key)]);
    return toNumber(r.returnValue) === 1;
  }

  /** Read a decision attestation's on-chain record (the tamper-evidence anchor). */
  async getAttestation(attestationId: string): Promise<AttestationRecord> {
    const [exists, ch, pv, v, t] = await Promise.all([
      this.contract.query(this.fn('get_attestation_exists'), [addressArg(attestationId)]),
      this.contract.query(this.fn('get_attestation_context_hash'), [addressArg(attestationId)]),
      this.contract.query(this.fn('get_attestation_policy_version'), [addressArg(attestationId)]),
      this.contract.query(this.fn('get_attestation_verdict'), [addressArg(attestationId)]),
      this.contract.query(this.fn('get_attestation_time'), [addressArg(attestationId)]),
    ]);
    return {
      exists: toNumber(exists.returnValue) === 1,
      contextHash: parseAddressReturn(ch.returnValue),
      policyVersion: toBigInt(pv.returnValue),
      verdict: toNumber(v.returnValue),
      time: toBigInt(t.returnValue),
    };
  }

  /** Fetch the complete state of a capability in one call. */
  async getCapabilityState(capabilityId: string): Promise<CapabilityState> {
    const [
      status,
      principal,
      agent,
      maxSpend,
      validUntil,
      totalSpent,
      actionCount,
      createdAt,
      revokedAt,
    ] = await Promise.all([
      this.getStatus(capabilityId),
      this.queryAddress('get_principal', capabilityId).catch(() => ''),
      this.queryAddress('get_agent', capabilityId).catch(() => ''),
      this.getMaxSpend(capabilityId),
      this.getValidUntil(capabilityId),
      this.getTotalSpent(capabilityId),
      this.getActionCount(capabilityId),
      this.getCreatedAt(capabilityId),
      this.getRevokedAt(capabilityId),
    ]);
    return {
      capabilityId,
      status,
      statusLabel: CAPABILITY_STATUS_LABELS[status] ?? 'not_found',
      principal,
      agent,
      maxSpend,
      validUntil,
      totalSpent,
      actionCount,
      createdAt,
      revokedAt,
    };
  }

  // ── Writes (signed; require a funded signer) ─────────────────────

  /** Principal mints a scoped capability to an agent. Signed by the principal. */
  mintPermission(opts: {
    signer: Signer;
    capabilityId: string;
    principal: string;
    agent: string;
    maxSpend: bigint;
    validUntil: bigint;
    allowlistEnabled?: boolean;
    currentTime: bigint;
    gasLimit?: number;
  }): Promise<SubmitReceipt> {
    return this.sendSigned(
      opts.signer,
      'mint_permission',
      [
        addressArg(opts.capabilityId),
        addressArg(opts.principal),
        addressArg(opts.agent),
        AbiEncode.i64(opts.maxSpend),
        AbiEncode.i64(opts.validUntil),
        AbiEncode.i64(opts.allowlistEnabled ? 1n : 0n),
        AbiEncode.i64(opts.currentTime),
      ],
      opts.gasLimit,
    );
  }

  /** Principal authorizes a counterparty for a capability's allowlist. */
  async addAllowedCounterparty(opts: {
    signer: Signer;
    capabilityId: string;
    counterparty: string;
    currentTime: bigint;
    gasLimit?: number;
  }): Promise<SubmitReceipt> {
    const key = await counterpartyKey(opts.capabilityId, opts.counterparty);
    return this.sendSigned(
      opts.signer,
      'add_allowed_counterparty',
      [addressArg(opts.capabilityId), addressArg(key), AbiEncode.i64(opts.currentTime)],
      opts.gasLimit,
    );
  }

  /** Principal updates a capability's policy (limits) and bumps its on-chain version. */
  updatePolicy(opts: {
    signer: Signer;
    capabilityId: string;
    newMaxSpend: bigint;
    newValidUntil: bigint;
    currentTime: bigint;
    gasLimit?: number;
  }): Promise<SubmitReceipt> {
    return this.sendSigned(
      opts.signer,
      'update_policy',
      [
        addressArg(opts.capabilityId),
        AbiEncode.i64(opts.newMaxSpend),
        AbiEncode.i64(opts.newValidUntil),
        AbiEncode.i64(opts.currentTime),
      ],
      opts.gasLimit,
    );
  }

  /**
   * Agent records a capability-bound action — the substrate Policy Enforcement
   * Point. Signed by the agent. `counterparty` is ignored when the capability's
   * allowlist is disabled; when enabled, the substrate refuses (code 5) unless
   * the counterparty has been allowlisted.
   */
  async recordAction(opts: {
    signer: Signer;
    capabilityId: string;
    amountSpent: bigint;
    counterparty?: string;
    currentTime: bigint;
    gasLimit?: number;
  }): Promise<SubmitReceipt> {
    const cp = opts.counterparty ?? BURN_SENTINEL;
    const key = await counterpartyKey(opts.capabilityId, cp);
    return this.sendSigned(
      opts.signer,
      'record_action',
      [
        addressArg(opts.capabilityId),
        AbiEncode.i64(opts.amountSpent),
        addressArg(key),
        AbiEncode.i64(opts.currentTime),
      ],
      opts.gasLimit,
    );
  }

  /** Principal revokes a capability. Terminal. Signed by the principal. */
  revokePermission(opts: {
    signer: Signer;
    capabilityId: string;
    currentTime: bigint;
    gasLimit?: number;
  }): Promise<SubmitReceipt> {
    return this.sendSigned(
      opts.signer,
      'revoke_permission',
      [addressArg(opts.capabilityId), AbiEncode.i64(opts.currentTime)],
      opts.gasLimit,
    );
  }

  /**
   * Agent records a decision attestation (provable-after). Commits the
   * decision's context_hash + verdict, bound to the capability's current policy
   * version. Signed by the agent.
   */
  recordAttestation(opts: {
    signer: Signer;
    attestationId: string;
    capabilityId: string;
    contextHash: string;
    verdict: number;
    currentTime: bigint;
    gasLimit?: number;
  }): Promise<SubmitReceipt> {
    return this.sendSigned(
      opts.signer,
      'record_attestation',
      [
        addressArg(opts.attestationId),
        addressArg(opts.capabilityId),
        addressArg(opts.contextHash),
        AbiEncode.i64(BigInt(opts.verdict)),
        AbiEncode.i64(opts.currentTime),
      ],
      opts.gasLimit,
    );
  }
}

/**
 * Parse an address return value from a contract query. The chain is uniformly
 * 32-byte (PQC, witness v2); we normalize to 32 bytes and re-encode to om1z. A
 * short hex value is zero-padded to 32 bytes (never 20) so a runtime returning
 * the wrong width surfaces as a clearly-wrong address, not a silently-valid one.
 */
export function parseAddressReturn(returnValue: ContractQueryResult['returnValue']): string {
  if (typeof returnValue === 'string' && /^(0x)?[0-9a-fA-F]+$/.test(returnValue)) {
    let hex = returnValue.replace(/^0x/, '');
    if (hex.length > 64) return returnValue; // unexpected width — surface raw, don't truncate
    hex = hex.padStart(64, '0');
    const bytes = new Uint8Array(32);
    for (let i = 0; i < 32; i++) {
      bytes[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
    }
    try {
      return encodeAddress(bytes);
    } catch {
      return returnValue;
    }
  }
  if (returnValue === '0' || returnValue === null) return '';
  return String(returnValue);
}

/**
 * @cinchor/sdk — Omne address + identifier encoding.
 *
 * The Omne chain is uniformly 32-byte (post-quantum). On-chain addresses and
 * contract map-key identifiers (capability ids, attestation ids) are bech32m
 * encodings of a 32-byte payload under witness version 2. This module produces
 * ABI arguments in that on-chain format and derives the deterministic ids the
 * accountability contract keys its state by.
 *
 * Format: bech32m("om", [0x02, ...toWords(32-byte payload)])  →  62-char "om1z…"
 */

import { bech32m } from '@scure/base';
import { ArgType, type AbiArgument } from '@omne/sdk';

const ADDRESS_HRP = 'om';
const WITNESS_VERSION = 2;

/** Decode an om1z bech32m address to its raw 32-byte payload. */
export function decodeAddress(address: string): Uint8Array {
  const { prefix, words } = bech32m.decode(address as `${string}1${string}`);
  if (prefix !== ADDRESS_HRP) {
    throw new Error(`Invalid address HRP: expected "${ADDRESS_HRP}", got "${prefix}"`);
  }
  if (words[0] !== WITNESS_VERSION) {
    throw new Error(`Invalid witness version: expected ${WITNESS_VERSION}, got ${words[0]}`);
  }
  return new Uint8Array(bech32m.fromWords(Array.from(words.slice(1))));
}

/** Encode a raw 32-byte payload as canonical om1z bech32m. Rejects non-32-byte input. */
export function encodeAddress(payload: Uint8Array): string {
  if (payload.length !== 32) {
    throw new Error(`Address payload must be 32 bytes, got ${payload.length}`);
  }
  const dataWords = bech32m.toWords(payload);
  const words = new Uint8Array(dataWords.length + 1);
  words[0] = WITNESS_VERSION;
  words.set(dataWords, 1);
  return bech32m.encode(ADDRESS_HRP, words);
}

/**
 * Build an ABI `Address` argument from an om1z string. Uses the 32-byte payload
 * directly — the substrate runtime accepts the Address discriminant + 32-byte
 * payload because that matches the on-chain representation.
 */
export function addressArg(address: string): AbiArgument {
  return { type: ArgType.Address, data: decodeAddress(address) };
}

async function sha256(bytes: Uint8Array): Promise<Uint8Array> {
  // Back the digest input with a concrete ArrayBuffer (TS narrows Uint8Array
  // over ArrayBufferLike, which does not satisfy BufferSource directly).
  const ab = new ArrayBuffer(bytes.byteLength);
  new Uint8Array(ab).set(bytes);
  return new Uint8Array(await crypto.subtle.digest('SHA-256', ab));
}

function u64be(value: number | bigint): Uint8Array {
  const b = new Uint8Array(8);
  new DataView(b.buffer).setBigUint64(0, BigInt(value), false);
  return b;
}

function concat(...parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((n, p) => n + p.length, 0);
  const out = new Uint8Array(total);
  let off = 0;
  for (const p of parts) {
    out.set(p, off);
    off += p.length;
  }
  return out;
}

/**
 * Derive a deterministic capability id from (principal, agent, nonce, createdAt).
 * Returns a 32-byte om1z address matching the chain's uniform width, so it
 * round-trips through the contract's address-typed map key.
 */
export async function deriveCapabilityId(
  principal: string,
  agent: string,
  nonce: number,
  createdAt: number,
): Promise<string> {
  const preimage = concat(
    decodeAddress(principal),
    decodeAddress(agent),
    u64be(nonce),
    u64be(createdAt),
  );
  return encodeAddress(await sha256(preimage));
}

/**
 * Derive the per-(capability, counterparty) allowlist key the contract stores
 * and checks. The contract is agnostic to the derivation — it only compares
 * stored keys — so the SDK owns it; minting an allowed counterparty and
 * enforcing an action MUST use the same derivation. Capability-scoped (the id
 * is in the preimage) so there is no cross-capability collision.
 */
export async function counterpartyKey(
  capabilityId: string,
  counterparty: string,
): Promise<string> {
  const preimage = concat(decodeAddress(capabilityId), decodeAddress(counterparty));
  return encodeAddress(await sha256(preimage));
}

export { sha256, u64be, concat };

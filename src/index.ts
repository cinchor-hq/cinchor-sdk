//
//    _  _                          _
//   | || |   __ _ _   _  __ _ _ __| |_ ___ _ __ ___
//   | || |_ / _` | | | |/ _` | '__| __/ _ \ '__/ __|
//   |__   _| (_| | |_| | (_| | |  | ||  __/ |  \__ \
//      |_|  \__, |\__,_|\__,_|_|   \__\___|_|  |___/
//              |_|
//
//   built by 4quarters
//

/**
 * @cinchor/sdk — accountability infrastructure for autonomous agents.
 *
 * Bound what an agent can do *before* it acts, and prove what it did *after* —
 * verifiably enough for a regulator, auditor, insurer, or court.
 *
 *   import { CinchorClient, IGNIS_LOCAL } from '@cinchor/sdk';
 *
 *   const cinchor = await CinchorClient.connect({
 *     network: IGNIS_LOCAL,
 *     contract: { name: 'cinchor_permissions', address: 'om1z…' },
 *   });
 *
 *   const { capabilityId } = await cinchor.mintCapability({
 *     principal, agent: agentAddress, maxSpend: 100n, ttlSeconds: 3600,
 *   });
 *
 *   const outcome = await cinchor.enforce({ capability: capabilityId, agent, amount: 40n });
 *   if (!outcome.allowed) throw new Error(`refused: ${outcome.reason}`);
 *
 *   await cinchor.attest({ capability: capabilityId, agent, context: decision });
 */

// Facade
export {
  CinchorClient,
  nowSecs,
  type MintCapabilityOptions,
  type MintCapabilityResult,
  type EnforceOptions,
  type AttestOptions,
  type AttestResult,
} from './client.js';

// Configuration
export {
  type CinchorConfig,
  type NetworkConfig,
  type ContractConfig,
  exportPrefixFor,
  IGNIS_LOCAL,
  BURN_SENTINEL,
  DEFAULT_GAS_LIMIT,
  DEFAULT_GAS_PRICE,
} from './config.js';

// Outcome types + enums
export {
  CapabilityStatus,
  CAPABILITY_STATUS_LABELS,
  EnforcementCode,
  ENFORCEMENT_LABELS,
  Verdict,
  type CapabilityStatusLabel,
  type EnforcementLabel,
  type Signer,
  type SubmitReceipt,
  type EnforcementOutcome,
  type CapabilityState,
  type AttestationRecord,
} from './types.js';

// Lower-level primitives (power users)
export { CapabilityRegistry, parseAddressReturn } from './capability-registry.js';

// Address + identifier utilities
export {
  encodeAddress,
  decodeAddress,
  addressArg,
  deriveCapabilityId,
  counterpartyKey,
} from './address.js';

// Attestation helpers
export {
  canonicalJson,
  hashDecisionContext,
  deriveAttestationId,
  verifyDecisionContext,
} from './attestation.js';

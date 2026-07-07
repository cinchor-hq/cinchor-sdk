/**
 * @cinchor/sdk — connection + deployment configuration.
 *
 * Everything that ties the SDK to a specific network and a specific deployed
 * accountability contract is injected here, not hardcoded. A consumer points
 * the SDK at their target network (devnet, testnet, mainnet) and the address of
 * the deployed contract they were given.
 */

/** Network coordinates the SDK talks to. */
export interface NetworkConfig {
  /** Human-readable network name (informational). */
  name: string;
  /** Omne chain id (Ignis devnet = 3). Signed into every transaction. */
  chainId: number;
  /** JSON-RPC endpoint URL. */
  rpcUrl: string;
}

/**
 * The deployed accountability contract the SDK calls.
 *
 * `name` is the on-chain contract module name; the runtime resolves calls via
 * the full export path `axiom_contract::<name>::<function>`, which `exportPrefix`
 * encodes. These are deployment details, not brand surfaces — point them at
 * whatever contract instance you were given.
 */
export interface ContractConfig {
  /** On-chain contract module name. */
  name: string;
  /** Deployed contract address (om1z bech32m). */
  address: string;
  /** Runtime export prefix: `axiom_contract::<name>::`. Derived if omitted. */
  exportPrefix?: string;
}

/** Full SDK configuration. */
export interface CinchorConfig {
  network: NetworkConfig;
  contract: ContractConfig;
  /** Default gas limit for write calls. */
  defaultGasLimit?: number;
  /** Default gas price (quar) for write calls. Ignis devnet base fee = 5000. */
  defaultGasPrice?: string;
}

/** Resolve a contract export prefix, deriving the default from the name. */
export function exportPrefixFor(contract: ContractConfig): string {
  return contract.exportPrefix ?? `axiom_contract::${contract.name}::`;
}

export const DEFAULT_GAS_LIMIT = 200_000;
export const DEFAULT_GAS_PRICE = '5000';

/**
 * A never-minted 32-byte sentinel capability id (witness v2, 32 bytes of 0xDE).
 * Used as the counterparty placeholder when allowlist enforcement is disabled.
 */
export const BURN_SENTINEL =
  'om1zmm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0qdtuxap';

/** Convenience preset for the local Ignis devnet. */
export const IGNIS_LOCAL: NetworkConfig = {
  name: 'ignis',
  chainId: 3,
  rpcUrl: 'http://127.0.0.1:26657',
};

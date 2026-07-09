"""Connection + deployment configuration for the Cinchor SDK.

Port of sdk-js/src/config.ts. Everything tying the SDK to a network and a
deployed accountability contract is injected here, not hardcoded.
"""

from __future__ import annotations

from dataclasses import dataclass

DEFAULT_GAS_LIMIT = 200_000
DEFAULT_GAS_PRICE = "5000"

# A never-minted 32-byte sentinel (witness v2, 32 bytes of 0xDE). Used as the
# counterparty placeholder when allowlist enforcement is disabled.
BURN_SENTINEL = "om1zmm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0qdtuxap"


@dataclass
class NetworkConfig:
    name: str
    chain_id: int
    rpc_url: str


@dataclass
class ContractConfig:
    name: str
    address: str
    export_prefix: str | None = None


@dataclass
class CinchorConfig:
    network: NetworkConfig
    contract: ContractConfig
    default_gas_limit: int | None = None
    default_gas_price: str | None = None


def export_prefix_for(contract: ContractConfig) -> str:
    """Runtime export prefix: ``axiom_contract::<name>::`` (derived if omitted)."""
    return contract.export_prefix or f"axiom_contract::{contract.name}::"


IGNIS_LOCAL = NetworkConfig(name="ignis", chain_id=3, rpc_url="http://127.0.0.1:26657")

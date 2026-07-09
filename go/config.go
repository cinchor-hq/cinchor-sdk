// Package cinchor is the Go SDK for Cinchor — accountability for autonomous
// agents on Omne (capability + attestation), built over the Omne base SDK.
// Parity-matched with the TS (@cinchor/sdk) and Python Cinchor SDKs: capability
// ids, counterparty keys, context hashes, and addresses derive identically.
//
// Proprietary — (c) DoneUp, Inc. All rights reserved. Not open source.
package cinchor

import "github.com/cinchor-hq/cinchor-sdk/go/nonce"

const (
	DefaultGasLimit = uint64(200_000)
	DefaultGasPrice = "5000"
	// BurnSentinel is a never-minted 32-byte sentinel (witness v2, 32 bytes of
	// 0xDE), used as the counterparty placeholder when allowlist is disabled.
	BurnSentinel = "om1zmm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0qdtuxap"
)

type NetworkConfig struct {
	Name    string
	ChainID int
	RPCURL  string
}

type ContractConfig struct {
	Name         string
	Address      string
	ExportPrefix string // derived as "axiom_contract::<name>::" if empty
}

type Config struct {
	Network         NetworkConfig
	Contract        ContractConfig
	DefaultGasLimit uint64 // 0 → DefaultGasLimit
	DefaultGasPrice string // "" → DefaultGasPrice
	// Nonces, when set, is the shared per-account nonce authority every write
	// routes through (serializes same-signer submits; distinct signers stay
	// parallel). nil → the SDK falls back to the base client's internal
	// NextNonce (standalone-SDK compatibility).
	Nonces *nonce.Manager
}

// ExportPrefixFor resolves the runtime export prefix for contract calls.
func ExportPrefixFor(c ContractConfig) string {
	if c.ExportPrefix != "" {
		return c.ExportPrefix
	}
	return "axiom_contract::" + c.Name + "::"
}

var IgnisLocal = NetworkConfig{Name: "ignis", ChainID: 3, RPCURL: "http://127.0.0.1:26657"}

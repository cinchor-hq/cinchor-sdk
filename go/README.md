# Cinchor Go SDK

Accountability for autonomous agents on Omne — the capability + attestation
layer over the Omne base SDK ([`github.com/OmneDAO/omne-sdks/go`](../../omne/sdk/go)).
Two verbs: `Enforce` (authorize-or-refuse, substrate-enforced) and `Attest`
(tamper-evident decision record), plus the capability lifecycle and audit reads.
Parity-matched with the TS (`@cinchor/sdk`) and Python Cinchor SDKs — ids,
counterparty keys, context hashes, and addresses derive identically.

> Proprietary — © DoneUp, Inc. All rights reserved. Not open source.

```go
import cinchor "github.com/cinchor-hq/cinchor/sdk-go"
```

## Quickstart
```go
pw, _ := omne.WalletFromMnemonic("…", "")
principal, _ := pw.Account(0)

c := cinchor.Connect(cinchor.Config{
    Network:  cinchor.NetworkConfig{Name: "ignis", ChainID: 3, RPCURL: "http://127.0.0.1:26657"},
    Contract: cinchor.ContractConfig{Name: "cinchor_permissions", Address: "om1z…"},
})

mint, _ := c.MintCapability(cinchor.MintOptions{Principal: principal, Agent: agentAddr, MaxSpend: 100, TTLSeconds: 3600})
out, _  := c.Enforce(cinchor.EnforceOptions{Capability: mint.CapabilityID, Agent: agent, Amount: 40}) // out.Allowed / out.Reason
att, _  := c.Attest(cinchor.AttestOptions{Capability: mint.CapabilityID, Agent: agent, Context: map[string]any{"decision": "…"}})
ok, _   := c.VerifyAttestation(map[string]any{"decision": "…"}, att.AttestationID)                    // ok.OK
```

## Layout
| File | Responsibility |
|---|---|
| `config.go` | network/contract config, `ExportPrefixFor`, `BurnSentinel` |
| `types.go` | `CapabilityStatus` / `EnforcementCode` / `Verdict` + state structs |
| `ids.go` | `DeriveCapabilityID`, `CounterpartyKey` (om1z, on the base SDK) |
| `attestation.go` | canonical-JSON context hashing, `DeriveAttestationID`, verify |
| `registry.go` | `CapabilityRegistry` — typed contract calls on `OmneClient` |
| `client.go` | `CinchorClient` — the two verbs + lifecycle + audit reads |

`CanonicalJSON` disables Go's HTML escaping and relies on `encoding/json`'s
map-key sorting so the bytes (hence the hash) match the TS/Python SDKs — pass a
`map[string]any` (not a struct) for decision contexts.

## Dependency
Depends on `github.com/OmneDAO/omne-sdks/go@v0.1.0`. That repo is **private**, so
`go get` needs `GOPRIVATE=github.com/OmneDAO/*` (or `github.com/cinchor-hq/*`) +
git auth.

## Validate
- `go test ./...` — offline cross-SDK parity (hash + id derivations vs Python).
- `examples/full_flow` — live-mesh proof: mint → enforce (in-bounds ×2,
  over-budget refused) → attest + verify (tamper-evident) → revoke → refused.
  `cd examples/full_flow && go run . <rpc> <contract> <wallets.json>`

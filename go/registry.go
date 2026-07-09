package cinchor

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	omne "github.com/OmneDAO/omne-sdks/go"
	"github.com/cinchor-hq/cinchor-sdk/go/nonce"
)

var hexRe = regexp.MustCompile(`^(0x)?[0-9a-fA-F]+$`)

// ParseAddressReturn decodes a contract address return: hex (0x-prefixed) → om1z;
// non-hex / "0" / nil → "". A short hex is zero-padded to 32 bytes so a
// wrong-width runtime surfaces as a clearly-wrong address, not a silent one.
func ParseAddressReturn(rv any) string {
	if s, ok := rv.(string); ok && hexRe.MatchString(s) {
		hexs := strings.TrimPrefix(s, "0x")
		if len(hexs) > 64 {
			return s
		}
		if len(hexs) < 64 {
			hexs = strings.Repeat("0", 64-len(hexs)) + hexs
		}
		b, err := omne.FromHex(hexs)
		if err != nil {
			return s
		}
		addr, err := omne.ToOmneAddress(b)
		if err != nil {
			return s
		}
		return addr
	}
	if rv == nil {
		return ""
	}
	if s, ok := rv.(string); ok && s == "0" {
		return ""
	}
	return fmt.Sprintf("%v", rv)
}

// toInt64 coerces a JSON-decoded returnValue (float64 for numbers) to int64.
// (Values here — status, spend, timestamps — fit in float64 without loss.)
func toInt64(rv any) int64 {
	switch v := rv.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

// arg builders: ArgAddress can error, so collect the first error while assembling.
type argFn func() (omne.AbiArgument, error)

func aAddr(s string) argFn { return func() (omne.AbiArgument, error) { return omne.ArgAddress(s) } }
func aI64(v int64) argFn   { return func() (omne.AbiArgument, error) { return omne.ArgI64(v), nil } }

func buildArgs(fns ...argFn) ([]omne.AbiArgument, error) {
	out := make([]omne.AbiArgument, len(fns))
	for i, f := range fns {
		a, err := f()
		if err != nil {
			return nil, err
		}
		out[i] = a
	}
	return out, nil
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// CapabilityRegistry is a typed wrapper over the deployed accountability
// contract, built on the Omne base SDK's OmneClient.
type CapabilityRegistry struct {
	client   *omne.OmneClient
	contract string
	prefix   string
	gasLimit uint64
	gasPrice string
	nonces   *nonce.Manager // shared nonce authority; nil → base client NextNonce
}

func NewCapabilityRegistry(cfg Config) *CapabilityRegistry {
	gl := cfg.DefaultGasLimit
	if gl == 0 {
		gl = DefaultGasLimit
	}
	gp := cfg.DefaultGasPrice
	if gp == "" {
		gp = DefaultGasPrice
	}
	return &CapabilityRegistry{
		client:   omne.NewOmneClient(cfg.Network.RPCURL, cfg.Network.ChainID),
		contract: cfg.Contract.Address,
		prefix:   ExportPrefixFor(cfg.Contract),
		gasLimit: gl,
		gasPrice: gp,
		nonces:   cfg.Nonces,
	}
}

func (r *CapabilityRegistry) fn(name string) string { return r.prefix + name }

func (r *CapabilityRegistry) send(signer *omne.WalletAccount, method string, args []omne.AbiArgument, gasLimit uint64) (SubmitReceipt, error) {
	if gasLimit == 0 {
		gasLimit = r.gasLimit
	}
	var tx string
	var err error
	if r.nonces != nil {
		// Route through the shared authority: it hands the closure the nonce to
		// sign at (passing &n takes SendContractCall's explicit-nonce branch,
		// bypassing the base client's own NextNonce). Receipt-waiting stays
		// OUTSIDE Submit so the per-signer lock is held only across sign+submit.
		tx, err = r.nonces.Submit(signer.Address, func(n uint64) (string, error) {
			return r.client.SendContractCall(signer, r.contract, r.fn(method), args, big.NewInt(0), gasLimit, r.gasPrice, &n)
		})
	} else {
		tx, err = r.client.SendContractCall(signer, r.contract, r.fn(method), args, big.NewInt(0), gasLimit, r.gasPrice, nil)
	}
	if err != nil {
		return SubmitReceipt{}, err
	}
	// No WaitForReceipt here: the caller's settle() is the single confirmation
	// (it polls committed state directly). The prior redundant 60s@1s receipt
	// wait was pure read amplification stacked under settle's own poll loop —
	// the settle-storm that saturated the node's read path. Return on
	// mempool-accept; settle establishes committed visibility.
	return SubmitReceipt{TransactionHash: tx, Status: "submitted"}, nil
}

// TxReceipt does a single non-blocking receipt check for the async confirmation
// path. included=false means the tx is not yet observable in a block ("not
// found" is expected while it propagates, not an error). success reflects
// whether the tx executed without a hard failure — NOT the contract return code
// (record_action returning a refusal code 2..6 still yields success=true), which
// is why callers confirm the outcome by committed state delta, not this flag.
func (r *CapabilityRegistry) TxReceipt(txHash string) (included, success bool, blockNumber int64, err error) {
	raw, e := r.client.GetTransactionReceipt(txHash)
	if e != nil {
		if strings.Contains(strings.ToLower(e.Error()), "not found") {
			return false, false, 0, nil // still propagating
		}
		return false, false, 0, e
	}
	if len(raw) == 0 || string(raw) == "null" {
		return false, false, 0, nil
	}
	var rec struct {
		Success     bool  `json:"success"`
		BlockNumber int64 `json:"blockNumber"`
	}
	if e := json.Unmarshal(raw, &rec); e != nil {
		return false, false, 0, fmt.Errorf("decode receipt: %w", e)
	}
	return true, rec.Success, rec.BlockNumber, nil
}

// ── writes (signed; require a funded signer) ─────────────────────────
func (r *CapabilityRegistry) MintPermission(signer *omne.WalletAccount, capabilityID, principal, agent string, maxSpend, validUntil int64, allowlistEnabled bool, currentTime int64, gasLimit uint64) (SubmitReceipt, error) {
	args, err := buildArgs(aAddr(capabilityID), aAddr(principal), aAddr(agent), aI64(maxSpend), aI64(validUntil), aI64(b2i(allowlistEnabled)), aI64(currentTime))
	if err != nil {
		return SubmitReceipt{}, err
	}
	return r.send(signer, "mint_permission", args, gasLimit)
}

func (r *CapabilityRegistry) AddAllowedCounterparty(signer *omne.WalletAccount, capabilityID, counterparty string, currentTime int64, gasLimit uint64) (SubmitReceipt, error) {
	key, err := CounterpartyKey(capabilityID, counterparty)
	if err != nil {
		return SubmitReceipt{}, err
	}
	args, err := buildArgs(aAddr(capabilityID), aAddr(key), aI64(currentTime))
	if err != nil {
		return SubmitReceipt{}, err
	}
	return r.send(signer, "add_allowed_counterparty", args, gasLimit)
}

func (r *CapabilityRegistry) UpdatePolicy(signer *omne.WalletAccount, capabilityID string, newMaxSpend, newValidUntil, currentTime int64, gasLimit uint64) (SubmitReceipt, error) {
	args, err := buildArgs(aAddr(capabilityID), aI64(newMaxSpend), aI64(newValidUntil), aI64(currentTime))
	if err != nil {
		return SubmitReceipt{}, err
	}
	return r.send(signer, "update_policy", args, gasLimit)
}

func (r *CapabilityRegistry) RecordAction(signer *omne.WalletAccount, capabilityID string, amountSpent int64, counterparty string, currentTime int64, actionID string, gasLimit uint64) (SubmitReceipt, error) {
	cp := counterparty
	if cp == "" {
		cp = BurnSentinel
	}
	key, err := CounterpartyKey(capabilityID, cp)
	if err != nil {
		return SubmitReceipt{}, err
	}
	args, err := buildArgs(aAddr(capabilityID), aI64(amountSpent), aAddr(key), aI64(currentTime), aAddr(actionID))
	if err != nil {
		return SubmitReceipt{}, err
	}
	return r.send(signer, "record_action", args, gasLimit)
}

func (r *CapabilityRegistry) RevokePermission(signer *omne.WalletAccount, capabilityID string, currentTime int64, gasLimit uint64) (SubmitReceipt, error) {
	args, err := buildArgs(aAddr(capabilityID), aI64(currentTime))
	if err != nil {
		return SubmitReceipt{}, err
	}
	return r.send(signer, "revoke_permission", args, gasLimit)
}

func (r *CapabilityRegistry) RecordAttestation(signer *omne.WalletAccount, attestationID, capabilityID, contextHash string, verdict Verdict, currentTime int64, gasLimit uint64) (SubmitReceipt, error) {
	args, err := buildArgs(aAddr(attestationID), aAddr(capabilityID), aAddr(contextHash), aI64(int64(verdict)), aI64(currentTime))
	if err != nil {
		return SubmitReceipt{}, err
	}
	return r.send(signer, "record_attestation", args, gasLimit)
}

// ── reads (no signer required) ───────────────────────────────────────
func (r *CapabilityRegistry) query(method, id string) (any, error) {
	arg, err := omne.ArgAddress(id)
	if err != nil {
		return nil, err
	}
	res, err := r.client.QueryContract(r.contract, r.fn(method), []omne.AbiArgument{arg}, "")
	if err != nil {
		return nil, err
	}
	return res["returnValue"], nil
}

func (r *CapabilityRegistry) queryInt(method, id string) (int64, error) {
	v, err := r.query(method, id)
	if err != nil {
		return 0, err
	}
	return toInt64(v), nil
}

func (r *CapabilityRegistry) GetStatus(cap string) (int64, error) { return r.queryInt("get_status", cap) }
func (r *CapabilityRegistry) GetMaxSpend(cap string) (int64, error) {
	return r.queryInt("get_max_spend", cap)
}
func (r *CapabilityRegistry) GetValidUntil(cap string) (int64, error) {
	return r.queryInt("get_valid_until", cap)
}
func (r *CapabilityRegistry) GetTotalSpent(cap string) (int64, error) {
	return r.queryInt("get_total_spent", cap)
}
func (r *CapabilityRegistry) GetActionCount(cap string) (int64, error) {
	return r.queryInt("get_action_count", cap)
}
func (r *CapabilityRegistry) GetCreatedAt(cap string) (int64, error) {
	return r.queryInt("get_created_at", cap)
}
func (r *CapabilityRegistry) GetRevokedAt(cap string) (int64, error) {
	return r.queryInt("get_revoked_at", cap)
}
func (r *CapabilityRegistry) GetPolicyVersion(cap string) (int64, error) {
	return r.queryInt("get_policy_version", cap)
}
func (r *CapabilityRegistry) GetAttestationCount(cap string) (int64, error) {
	return r.queryInt("get_attestation_count", cap)
}

// GetAttestationExists is a single-read existence check (one omne_call), used to
// settle Attest cheaply instead of the multi-read GetAttestation fan-out.
func (r *CapabilityRegistry) GetAttestationExists(attestationID string) (bool, error) {
	v, err := r.queryInt("get_attestation_exists", attestationID)
	if err != nil {
		return false, err
	}
	return v > 0, nil
}

func (r *CapabilityRegistry) GetAllowlistEnabled(cap string) (bool, error) {
	v, err := r.queryInt("get_allowlist_enabled", cap)
	return v == 1, err
}

func (r *CapabilityRegistry) IsCounterpartyAllowed(cap, counterparty string) (bool, error) {
	key, err := CounterpartyKey(cap, counterparty)
	if err != nil {
		return false, err
	}
	v, err := r.queryInt("is_counterparty_allowed", key)
	return v == 1, err
}

func (r *CapabilityRegistry) GetAttestation(attestationID string) (AttestationRecord, error) {
	exists, err := r.queryInt("get_attestation_exists", attestationID)
	if err != nil {
		return AttestationRecord{}, err
	}
	chRaw, err := r.query("get_attestation_context_hash", attestationID)
	if err != nil {
		return AttestationRecord{}, err
	}
	pv, err := r.queryInt("get_attestation_policy_version", attestationID)
	if err != nil {
		return AttestationRecord{}, err
	}
	v, err := r.queryInt("get_attestation_verdict", attestationID)
	if err != nil {
		return AttestationRecord{}, err
	}
	t, err := r.queryInt("get_attestation_time", attestationID)
	if err != nil {
		return AttestationRecord{}, err
	}
	return AttestationRecord{
		Exists:        exists == 1,
		ContextHash:   ParseAddressReturn(chRaw),
		PolicyVersion: pv,
		Verdict:       int(v),
		Time:          t,
	}, nil
}

func (r *CapabilityRegistry) GetCapabilityState(cap string) (CapabilityState, error) {
	status, err := r.GetStatus(cap)
	if err != nil {
		return CapabilityState{}, err
	}
	safeAddr := func(method string) string {
		v, err := r.query(method, cap)
		if err != nil {
			return ""
		}
		return ParseAddressReturn(v)
	}
	get := func(f func(string) (int64, error)) (int64, error) { return f(cap) }
	maxSpend, err := get(r.GetMaxSpend)
	if err != nil {
		return CapabilityState{}, err
	}
	validUntil, err := get(r.GetValidUntil)
	if err != nil {
		return CapabilityState{}, err
	}
	totalSpent, err := get(r.GetTotalSpent)
	if err != nil {
		return CapabilityState{}, err
	}
	actionCount, err := get(r.GetActionCount)
	if err != nil {
		return CapabilityState{}, err
	}
	createdAt, err := get(r.GetCreatedAt)
	if err != nil {
		return CapabilityState{}, err
	}
	revokedAt, err := get(r.GetRevokedAt)
	if err != nil {
		return CapabilityState{}, err
	}
	label := "not_found"
	if status >= 0 && int(status) < len(CapabilityStatusLabels) {
		label = CapabilityStatusLabels[status]
	}
	return CapabilityState{
		CapabilityID: cap,
		Status:       CapabilityStatus(status),
		StatusLabel:  label,
		Principal:    safeAddr("get_principal"),
		Agent:        safeAddr("get_agent"),
		MaxSpend:     maxSpend,
		ValidUntil:   validUntil,
		TotalSpent:   totalSpent,
		ActionCount:  actionCount,
		CreatedAt:    createdAt,
		RevokedAt:    revokedAt,
	}, nil
}

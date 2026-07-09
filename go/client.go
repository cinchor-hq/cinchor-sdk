package cinchor

import (
	"crypto/rand"
	"fmt"
	"time"

	omne "github.com/OmneDAO/omne-sdks/go"
)

// NowSecs is the current UNIX time in seconds.
func NowSecs() int64 { return time.Now().Unix() }

func randNonce() uint64 {
	var b [6]byte // 48-bit, matching the TS default range
	_, _ = rand.Read(b[:])
	var n uint64
	for _, x := range b {
		n = n<<8 | uint64(x)
	}
	return n
}

// settle polls read() until done() holds or the timeout elapses — makes
// read-after-write deterministic on a multi-validator mesh.
func settle[T any](read func() (T, error), done func(T) bool, timeout, interval time.Duration) (T, error) {
	deadline := time.Now().Add(timeout)
	last, err := read()
	if err != nil {
		return last, err
	}
	for !done(last) && time.Now().Before(deadline) {
		time.Sleep(interval)
		last, err = read()
		if err != nil {
			return last, err
		}
	}
	return last, nil
}

// CinchorClient is the high-level facade: the two verbs (enforce/attest) plus
// the capability lifecycle and audit reads.
type CinchorClient struct {
	Registry *CapabilityRegistry
}

func Connect(cfg Config) *CinchorClient {
	return &CinchorClient{Registry: NewCapabilityRegistry(cfg)}
}

// ── capability lifecycle ─────────────────────────────────────────────
type MintOptions struct {
	Principal   *omne.WalletAccount
	Agent       string
	MaxSpend    int64
	ValidUntil  int64 // 0 → derive from TTLSeconds
	TTLSeconds  int64 // used when ValidUntil == 0
	Allowlist   bool
	Nonce       uint64 // 0 → random
	CurrentTime int64  // 0 → now
	GasLimit    uint64
}

type MintResult struct {
	CapabilityID string
	Receipt      SubmitReceipt
}

// PrepareMint fills in the derived fields (nonce, valid_until, current_time) and
// the DETERMINISTIC capability id WITHOUT submitting. The async request path
// returns the id immediately and freezes the completed options so the worker
// submits byte-identically (a resubmit re-mints the same token_id — first-write).
func (c *CinchorClient) PrepareMint(o MintOptions) (capabilityID string, frozen MintOptions, err error) {
	if o.CurrentTime == 0 {
		o.CurrentTime = NowSecs()
	}
	if o.ValidUntil == 0 {
		o.ValidUntil = o.CurrentTime + o.TTLSeconds
	}
	if o.Nonce == 0 {
		o.Nonce = randNonce()
	}
	cap, err := DeriveCapabilityID(o.Principal.Address, o.Agent, o.Nonce, uint64(o.CurrentTime))
	if err != nil {
		return "", MintOptions{}, err
	}
	return cap, o, nil
}

// SubmitMint submits mint_permission on mempool-accept (no settle). o must be
// frozen (nonce + current_time + valid_until set).
func (c *CinchorClient) SubmitMint(o MintOptions) (capabilityID string, receipt SubmitReceipt, err error) {
	cap, err := DeriveCapabilityID(o.Principal.Address, o.Agent, o.Nonce, uint64(o.CurrentTime))
	if err != nil {
		return "", SubmitReceipt{}, err
	}
	receipt, err = c.Registry.MintPermission(o.Principal, cap, o.Principal.Address, o.Agent, o.MaxSpend, o.ValidUntil, o.Allowlist, o.CurrentTime, o.GasLimit)
	return cap, receipt, err
}

// ConfirmMint reports whether the capability is committed (active). Mint has no
// on-chain refusal for a fresh derived id (first-write), so it is committed or
// still propagating.
func (c *CinchorClient) ConfirmMint(capabilityID string) (ConfirmStatus, error) {
	status, err := c.Registry.GetStatus(capabilityID)
	if err != nil {
		return ConfirmPending, err
	}
	if status == int64(StatusActive) {
		return ConfirmCommitted, nil
	}
	return ConfirmPending, nil
}

// MintCapability is the back-compat blocking mint (Prepare → Submit → poll
// Confirm) for standalone callers.
func (c *CinchorClient) MintCapability(o MintOptions) (MintResult, error) {
	cap, frozen, err := c.PrepareMint(o)
	if err != nil {
		return MintResult{}, err
	}
	_, receipt, err := c.SubmitMint(frozen)
	if err != nil {
		return MintResult{}, err
	}
	deadline := time.Now().Add(12 * time.Second)
	for {
		st, err := c.ConfirmMint(cap)
		if err != nil {
			return MintResult{}, err
		}
		if st == ConfirmCommitted || time.Now().After(deadline) {
			break
		}
		time.Sleep(750 * time.Millisecond)
	}
	return MintResult{CapabilityID: cap, Receipt: receipt}, nil
}

func (c *CinchorClient) Revoke(capability string, principal *omne.WalletAccount, currentTime int64, gasLimit uint64) (SubmitReceipt, error) {
	if currentTime == 0 {
		currentTime = NowSecs()
	}
	receipt, err := c.Registry.RevokePermission(principal, capability, currentTime, gasLimit)
	if err != nil {
		return SubmitReceipt{}, err
	}
	_, _ = settle(
		func() (int64, error) { return c.Registry.GetStatus(capability) },
		func(status int64) bool { return status == int64(StatusRevoked) },
		12*time.Second, 750*time.Millisecond,
	)
	return receipt, nil
}

func (c *CinchorClient) AllowCounterparty(capability string, principal *omne.WalletAccount, counterparty string, currentTime int64, gasLimit uint64) (SubmitReceipt, error) {
	if currentTime == 0 {
		currentTime = NowSecs()
	}
	return c.Registry.AddAllowedCounterparty(principal, capability, counterparty, currentTime, gasLimit)
}

func (c *CinchorClient) UpdatePolicy(capability string, principal *omne.WalletAccount, maxSpend, validUntil, currentTime int64, gasLimit uint64) (SubmitReceipt, error) {
	if currentTime == 0 {
		currentTime = NowSecs()
	}
	return c.Registry.UpdatePolicy(principal, capability, maxSpend, validUntil, currentTime, gasLimit)
}

// ── the two verbs ────────────────────────────────────────────────────
type EnforceOptions struct {
	Capability   string
	Agent        *omne.WalletAccount
	Amount       int64
	Counterparty string
	CurrentTime  int64
	GasLimit     uint64
	// ActionID is the on-chain idempotency key. Leave empty for a one-shot call
	// (derived per-submit); the async worker sets a STABLE value (frozen on the
	// operation) so a crash-resubmit dedups to exactly-once.
	ActionID string
}

// Enforce is the back-compat synchronous call: instant verdict, submit if
// allowed, block until confirmed. Kept for standalone SDK/CLI users; the async
// gateway path uses Prejudge + SubmitEnforce + ConfirmEnforce instead.
func (c *CinchorClient) Enforce(o EnforceOptions) (EnforcementOutcome, error) {
	return c.EnforceAndConfirm(o)
}

// Prejudge is the CONTROL-BEFORE decision: it evaluates the verdict from
// committed pre-state WITHOUT submitting anything, and returns the pre-state so
// the caller can freeze baselines. A positive-evidence deny (revoked / expired /
// over_budget / out_of_allowlist on a committed capability) is authoritative and
// final — refuse instantly. A NotFound is AMBIGUOUS (a freshly minted capability
// may not be visible yet; absence is not evidence), so the caller should still
// submit and let confirmation absorb the visibility lag. This is a reads-only,
// no-gas, no-tx operation — the instant half of the two-phase model.
func (c *CinchorClient) Prejudge(o EnforceOptions) (CapabilityState, EnforcementCode, error) {
	t := o.CurrentTime
	if t == 0 {
		t = NowSecs()
	}
	before, err := c.Registry.GetCapabilityState(o.Capability)
	if err != nil {
		return CapabilityState{}, CodeNotFound, err
	}
	code, err := c.prejudge(before, o.Amount, t, o.Capability, o.Counterparty)
	return before, code, err
}

// SubmitEnforce submits record_action to the mempool as the bound agent and
// returns on mempool-accept (no settle). The async worker calls this with the
// frozen inputs (incl. a stable ActionID) from the persisted operation so a
// resubmit dedups on-chain to exactly-once.
func (c *CinchorClient) SubmitEnforce(o EnforceOptions) (SubmitReceipt, error) {
	t := o.CurrentTime
	if t == 0 {
		t = NowSecs()
	}
	aid := o.ActionID
	if aid == "" {
		// One-shot call: a fresh unique id (no resubmit, so uniqueness suffices).
		var err error
		aid, err = DeriveActionID(o.Capability, uint64(t), uint64(o.Amount), randNonce())
		if err != nil {
			return SubmitReceipt{}, err
		}
	}
	return c.Registry.RecordAction(o.Agent, o.Capability, o.Amount, o.Counterparty, t, aid, o.GasLimit)
}

// ConfirmStatus is the proof-after confirmation state of a submitted enforce.
type ConfirmStatus int

const (
	ConfirmPending    ConfirmStatus = iota // tx not yet observable in a block — retry
	ConfirmCommitted                       // committed as allowed (delta observed)
	ConfirmRefused                         // tx included but the contract refused it (terminal)
	ConfirmExecFailed                      // tx execution failed (e.g. out of gas) — retryable
)

// ConfirmEnforceOptions is the frozen state the worker replays to confirm an
// enforce it submitted (reconstructable from the persisted operation row across
// a crash — no in-memory handle needed).
type ConfirmEnforceOptions struct {
	Capability          string
	TxHash              string
	BaselineActionCount int64
	BaselineTotalSpent  int64
	Amount              int64
	CurrentTime         int64
	Counterparty        string
}

// ConfirmEnforce is the PROOF-AFTER confirmation. It reads committed state and,
// because the single-writer-per-capability invariant guarantees no other
// in-flight enforce, attributes any (+1 count, +amount spent) delta to THIS
// operation — no aggregate-delta aliasing. Committed → allowed. If no delta but
// the tx is included and succeeded, the contract refused it (re-prejudge names
// the terminal reason — surfaces a revoke/tighten that landed mid-flight). Not
// yet included → pending (the worker retries).
func (c *CinchorClient) ConfirmEnforce(o ConfirmEnforceOptions) (EnforcementOutcome, ConfirmStatus, error) {
	after, err := c.Registry.GetCapabilityState(o.Capability)
	if err != nil {
		return EnforcementOutcome{}, ConfirmPending, err
	}
	if after.ActionCount == o.BaselineActionCount+1 && after.TotalSpent == o.BaselineTotalSpent+o.Amount {
		return EnforcementOutcome{Allowed: true, Code: CodeAllowed, Reason: EnforcementLabels[CodeAllowed]}, ConfirmCommitted, nil
	}
	if o.TxHash == "" {
		// Nothing submitted yet (reconcile-first on a never-submitted op) and no
		// delta — the caller should submit.
		return EnforcementOutcome{}, ConfirmPending, nil
	}
	included, success, _, err := c.Registry.TxReceipt(o.TxHash)
	if err != nil {
		return EnforcementOutcome{}, ConfirmPending, err
	}
	if !included {
		return EnforcementOutcome{}, ConfirmPending, nil // still propagating
	}
	if !success {
		return EnforcementOutcome{}, ConfirmExecFailed, nil // gas/exec failure — resubmittable
	}
	// Included + succeeded but no allowed-delta ⇒ the contract refused the action.
	// Name the reason from committed state (the mid-flight-revoke breach surfaces here).
	code, err := c.prejudge(after, o.Amount, o.CurrentTime, o.Capability, o.Counterparty)
	if err != nil {
		return EnforcementOutcome{}, ConfirmPending, err
	}
	// CodeAllowed: succeeded but our delta isn't visible yet (look again).
	// CodeNotFound: the capability isn't committed yet — a mint-then-enforce
	// where the mint is still in flight, or a visibility lag. Both retry rather
	// than fail; only a POSITIVE refusal (revoked/expired/over_budget/
	// out_of_allowlist) is terminal.
	if code == CodeAllowed || code == CodeNotFound {
		return EnforcementOutcome{}, ConfirmPending, nil
	}
	return EnforcementOutcome{Allowed: false, Code: code, Reason: EnforcementLabels[code]}, ConfirmRefused, nil
}

// EnforceAndConfirm reproduces the old synchronous Enforce from the decomposed
// pieces: instant Prejudge, submit if allowed, then poll ConfirmEnforce until
// terminal. For standalone SDK/CLI callers who want a blocking call.
func (c *CinchorClient) EnforceAndConfirm(o EnforceOptions) (EnforcementOutcome, error) {
	t := o.CurrentTime
	if t == 0 {
		t = NowSecs()
		o.CurrentTime = t
	}
	before, code, err := c.Prejudge(o)
	if err != nil {
		return EnforcementOutcome{}, err
	}
	if code != CodeAllowed && before.Status != StatusNotFound {
		return EnforcementOutcome{Allowed: false, Code: code, Reason: EnforcementLabels[code]}, nil
	}
	receipt, err := c.SubmitEnforce(o)
	if err != nil {
		return EnforcementOutcome{}, err
	}
	co := ConfirmEnforceOptions{
		Capability: o.Capability, TxHash: receipt.TransactionHash,
		BaselineActionCount: before.ActionCount, BaselineTotalSpent: before.TotalSpent,
		Amount: o.Amount, CurrentTime: t, Counterparty: o.Counterparty,
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		out, status, err := c.ConfirmEnforce(co)
		if err != nil {
			return EnforcementOutcome{}, err
		}
		switch status {
		case ConfirmCommitted:
			out.Receipt = receipt
			return out, nil
		case ConfirmRefused:
			return out, nil
		case ConfirmExecFailed:
			return EnforcementOutcome{}, fmt.Errorf("enforce tx execution failed for %s (tx %s)", o.Capability, receipt.TransactionHash)
		}
		if time.Now().After(deadline) {
			return EnforcementOutcome{}, fmt.Errorf("enforce unsettled: action on %s not confirmed within settle window (tx %s)", o.Capability, receipt.TransactionHash)
		}
		time.Sleep(750 * time.Millisecond)
	}
}

// prejudge evaluates the deny codes against the pre-state, in the contract's
// precedence order. CodeAllowed here means "nothing refuses it pre-submit".
func (c *CinchorClient) prejudge(before CapabilityState, amount, t int64, capability, counterparty string) (EnforcementCode, error) {
	if before.Status == StatusNotFound {
		return CodeNotFound, nil
	}
	if before.Status == StatusRevoked {
		return CodeRevoked, nil
	}
	if t > before.ValidUntil {
		return CodeExpired, nil
	}
	if before.TotalSpent+amount > before.MaxSpend {
		return CodeOverBudget, nil
	}
	enabled, err := c.Registry.GetAllowlistEnabled(capability)
	if err != nil {
		return CodeNotFound, err
	}
	if enabled {
		cp := counterparty
		if cp == "" {
			cp = BurnSentinel
		}
		allowed, err := c.Registry.IsCounterpartyAllowed(capability, cp)
		if err != nil {
			return CodeNotFound, err
		}
		if !allowed {
			return CodeOutOfAllowlist, nil
		}
	}
	return CodeAllowed, nil
}

type AttestOptions struct {
	Capability  string
	Agent       *omne.WalletAccount
	Context     any
	Verdict     Verdict // 0 → InPolicy
	Seq         uint64
	CurrentTime int64
	GasLimit    uint64
}

type AttestResult struct {
	AttestationID string
	ContextHash   string
	Verdict       Verdict
	Receipt       SubmitReceipt
}

// PrepareAttest computes the (salted-context) hash + deterministic attestation
// id WITHOUT submitting. The request path returns the id + freezes the options.
func (c *CinchorClient) PrepareAttest(o AttestOptions) (attestationID, contextHash string, frozen AttestOptions, err error) {
	if o.CurrentTime == 0 {
		o.CurrentTime = NowSecs()
	}
	if o.Verdict == 0 {
		o.Verdict = VerdictInPolicy
	}
	ch, err := HashDecisionContext(o.Context)
	if err != nil {
		return "", "", AttestOptions{}, err
	}
	aid, err := DeriveAttestationID(o.Capability, ch, o.Seq)
	if err != nil {
		return "", "", AttestOptions{}, err
	}
	return aid, ch, o, nil
}

// SubmitAttest submits record_attestation on mempool-accept (no settle), from
// the frozen attestationID/contextHash so a resubmit is byte-identical
// (first-write on attestation_id).
func (c *CinchorClient) SubmitAttest(o AttestOptions, attestationID, contextHash string) (SubmitReceipt, error) {
	return c.Registry.RecordAttestation(o.Agent, attestationID, o.Capability, contextHash, o.Verdict, o.CurrentTime, o.GasLimit)
}

// ConfirmAttest reports whether the attestation is committed (exists on-chain).
func (c *CinchorClient) ConfirmAttest(attestationID string) (ConfirmStatus, error) {
	exists, err := c.Registry.GetAttestationExists(attestationID)
	if err != nil {
		return ConfirmPending, err
	}
	if exists {
		return ConfirmCommitted, nil
	}
	return ConfirmPending, nil
}

// Attest is the back-compat blocking attest (Prepare → Submit → poll Confirm).
func (c *CinchorClient) Attest(o AttestOptions) (AttestResult, error) {
	aid, ch, frozen, err := c.PrepareAttest(o)
	if err != nil {
		return AttestResult{}, err
	}
	receipt, err := c.SubmitAttest(frozen, aid, ch)
	if err != nil {
		return AttestResult{}, err
	}
	deadline := time.Now().Add(12 * time.Second)
	for {
		st, err := c.ConfirmAttest(aid)
		if err != nil {
			return AttestResult{}, err
		}
		if st == ConfirmCommitted || time.Now().After(deadline) {
			break
		}
		time.Sleep(750 * time.Millisecond)
	}
	return AttestResult{AttestationID: aid, ContextHash: ch, Verdict: frozen.Verdict, Receipt: receipt}, nil
}

// ── audit (reads) ────────────────────────────────────────────────────
func (c *CinchorClient) GetCapability(capabilityID string) (CapabilityState, error) {
	return c.Registry.GetCapabilityState(capabilityID)
}

func (c *CinchorClient) GetAttestation(attestationID string) (AttestationRecord, error) {
	return c.Registry.GetAttestation(attestationID)
}

type VerifyResult struct {
	OK         bool
	Recomputed string
	OnChain    AttestationRecord
}

func (c *CinchorClient) VerifyAttestation(context any, attestationID string) (VerifyResult, error) {
	onChain, err := c.Registry.GetAttestation(attestationID)
	if err != nil {
		return VerifyResult{}, err
	}
	ok, recomputed, err := VerifyDecisionContext(context, onChain.ContextHash)
	if err != nil {
		return VerifyResult{}, err
	}
	return VerifyResult{OK: ok && onChain.Exists, Recomputed: recomputed, OnChain: onChain}, nil
}

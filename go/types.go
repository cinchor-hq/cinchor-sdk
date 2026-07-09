package cinchor

// CapabilityStatus is the lifecycle state of a capability (permission token).
type CapabilityStatus int

const (
	StatusNotFound CapabilityStatus = 0
	StatusActive   CapabilityStatus = 1
	StatusRevoked  CapabilityStatus = 2
)

var CapabilityStatusLabels = []string{"not_found", "active", "revoked"}

// EnforcementCode is the substrate's authorize-or-refuse verdict for an action.
// Code 1 is the only allow; every other code is a substrate-enforced refusal.
type EnforcementCode int

const (
	CodeNotFound       EnforcementCode = 0
	CodeAllowed        EnforcementCode = 1
	CodeRevoked        EnforcementCode = 2
	CodeExpired        EnforcementCode = 3
	CodeOverBudget     EnforcementCode = 4
	CodeOutOfAllowlist EnforcementCode = 5
	// CodeUnauthorized is returned by record_action when the caller is not the
	// token's designated agent (on-chain caller binding). Legitimate gateway/SDK
	// callers sign with the agent key and never see it; it surfaces only on a
	// forged/misdirected call, and is a WRITE-FREE refusal on-chain.
	CodeUnauthorized EnforcementCode = 6
)

var EnforcementLabels = map[EnforcementCode]string{
	CodeNotFound:       "not_found",
	CodeAllowed:        "allowed",
	CodeRevoked:        "revoked",
	CodeExpired:        "expired",
	CodeOverBudget:     "over_budget",
	CodeOutOfAllowlist: "out_of_allowlist",
	CodeUnauthorized:   "unauthorized",
}

// Verdict committed alongside a decision attestation.
type Verdict int

const (
	VerdictInPolicy    Verdict = 1
	VerdictOutOfPolicy Verdict = 2
)

// SubmitReceipt is the result of a committed (or pending) write.
type SubmitReceipt struct {
	TransactionHash string
	Status          string
	BlockNumber     *int64
}

// CapabilityState is the full on-chain state of a capability.
type CapabilityState struct {
	CapabilityID string
	Status       CapabilityStatus
	StatusLabel  string
	Principal    string
	Agent        string
	MaxSpend     int64
	ValidUntil   int64
	TotalSpent   int64
	ActionCount  int64
	CreatedAt    int64
	RevokedAt    int64
}

// AttestationRecord is the on-chain record of a decision attestation.
type AttestationRecord struct {
	Exists        bool
	ContextHash   string
	PolicyVersion int64
	Verdict       int
	Time          int64
}

// EnforcementOutcome is the result of an enforce() call.
type EnforcementOutcome struct {
	Allowed bool
	Code    EnforcementCode
	Reason  string
	Receipt SubmitReceipt
}

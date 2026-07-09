package cinchor

import (
	"crypto/sha256"
	"encoding/binary"

	omne "github.com/OmneDAO/omne-sdks/go"
)

func u64be(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return b
}

// DeriveCapabilityID = sha256(principal ‖ agent ‖ u64be(nonce) ‖ u64be(createdAt)) → om1z.
// The contract is agnostic to this derivation; it must match the TS/Python SDKs.
func DeriveCapabilityID(principal, agent string, nonce, createdAt uint64) (string, error) {
	p, err := omne.FromOmneAddress(principal)
	if err != nil {
		return "", err
	}
	a, err := omne.FromOmneAddress(agent)
	if err != nil {
		return "", err
	}
	pre := make([]byte, 0, 32+32+16)
	pre = append(pre, p...)
	pre = append(pre, a...)
	pre = append(pre, u64be(nonce)...)
	pre = append(pre, u64be(createdAt)...)
	h := sha256.Sum256(pre)
	return omne.ToOmneAddress(h[:])
}

// DeriveActionID = sha256("cinchor.action" ‖ capabilityID ‖ u64be(currentTime) ‖
// u64be(amount) ‖ u64be(seq)) → om1z. The on-chain idempotency key for
// record_action: it must be STABLE for a given logical action so a worker
// resubmit (after a crash, possibly with a reseeded nonce) dedups to
// exactly-once. The gateway derives it once (seq = a per-op nonce), freezes it
// on the operation, and replays the same value; the standalone blocking path
// derives a fresh one per call (no resubmit, so uniqueness suffices).
func DeriveActionID(capabilityID string, currentTime, amount, seq uint64) (string, error) {
	c, err := omne.FromOmneAddress(capabilityID)
	if err != nil {
		return "", err
	}
	pre := make([]byte, 0, 14+32+24)
	pre = append(pre, []byte("cinchor.action")...)
	pre = append(pre, c...)
	pre = append(pre, u64be(currentTime)...)
	pre = append(pre, u64be(amount)...)
	pre = append(pre, u64be(seq)...)
	h := sha256.Sum256(pre)
	return omne.ToOmneAddress(h[:])
}

// CounterpartyKey = sha256(capabilityID ‖ counterparty) → om1z. Capability-scoped;
// minting an allowed counterparty and enforcing an action use the same derivation.
func CounterpartyKey(capabilityID, counterparty string) (string, error) {
	c, err := omne.FromOmneAddress(capabilityID)
	if err != nil {
		return "", err
	}
	cp, err := omne.FromOmneAddress(counterparty)
	if err != nil {
		return "", err
	}
	pre := make([]byte, 0, 64)
	pre = append(pre, c...)
	pre = append(pre, cp...)
	h := sha256.Sum256(pre)
	return omne.ToOmneAddress(h[:])
}

package cinchor

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"strings"

	omne "github.com/OmneDAO/omne-sdks/go"
)

// CanonicalJSON produces compact JSON with sorted keys and HTML escaping OFF,
// matching the TS SDK (JSON.stringify over sorted-key-normalized values) and
// the Python SDK. Go's encoding/json already sorts map keys; disabling HTML
// escaping makes <, >, & encode literally so the bytes match across SDKs.
//
// Pass a map[string]any (or JSON-object-shaped value) so keys are sorted; struct
// field order is NOT sorted by encoding/json and would not match the other SDKs.
func CanonicalJSON(value any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// HashDecisionContext hashes a decision-context object to a 32-byte om1z commitment.
func HashDecisionContext(context any) (string, error) {
	cj, err := CanonicalJSON(context)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(cj))
	return omne.ToOmneAddress(h[:])
}

// DeriveAttestationID = sha256(capabilityID ‖ contextHash ‖ u64be(seq)) → om1z.
func DeriveAttestationID(capabilityID, contextHash string, seq uint64) (string, error) {
	c, err := omne.FromOmneAddress(capabilityID)
	if err != nil {
		return "", err
	}
	ch, err := omne.FromOmneAddress(contextHash)
	if err != nil {
		return "", err
	}
	pre := make([]byte, 0, 32+32+8)
	pre = append(pre, c...)
	pre = append(pre, ch...)
	pre = append(pre, u64be(seq)...)
	h := sha256.Sum256(pre)
	return omne.ToOmneAddress(h[:])
}

// VerifyDecisionContext re-hashes a context and confirms it matches the on-chain
// commitment. Returns (ok, recomputed).
func VerifyDecisionContext(context any, onChainContextHash string) (bool, string, error) {
	recomputed, err := HashDecisionContext(context)
	if err != nil {
		return false, "", err
	}
	return recomputed == onChainContextHash, recomputed, nil
}

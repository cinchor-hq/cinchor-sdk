package cinchor

import "testing"

// Cross-SDK parity vectors — ground truth from the Python Cinchor SDK for
// identical inputs. These pin the canonical-JSON/hash and id derivations so a
// capability/attestation produced by one SDK is addressable + verifiable by
// another. (BURN_SENTINEL used as a convenient fixed 32-byte om1z input.)
const (
	burn       = "om1zmm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0qdtuxap"
	expHashCtx = "om1zdl4nwuamsk92zsjdcfvrh9ldzuggptuajzk2c6wetfdwr05q0zdsqmztax"
	expCapID   = "om1zmll2zr2geq3az6j994n8p9sp6vap23k39wtfk9ddwgt26hvfpdxq44fsyh"
	expCPKey   = "om1zvm6ptu0c622d2kffhtkgc2uej0v6grwymfv5u6yyurh3lk67ncjsuystqd"
)

func TestHashDecisionContextParity(t *testing.T) {
	ctx := map[string]any{
		"model":  "x",
		"input":  map[string]any{"claim": "A"},
		"output": "ok",
		"at":     1700000000,
	}
	got, err := HashDecisionContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != expHashCtx {
		t.Fatalf("hash_decision_context = %s, want %s", got, expHashCtx)
	}
}

func TestDeriveCapabilityIDParity(t *testing.T) {
	got, err := DeriveCapabilityID(burn, burn, 1, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if got != expCapID {
		t.Fatalf("derive_capability_id = %s, want %s", got, expCapID)
	}
}

func TestCounterpartyKeyParity(t *testing.T) {
	got, err := CounterpartyKey(burn, burn)
	if err != nil {
		t.Fatal(err)
	}
	if got != expCPKey {
		t.Fatalf("counterparty_key = %s, want %s", got, expCPKey)
	}
}

func TestExportPrefix(t *testing.T) {
	got := ExportPrefixFor(ContractConfig{Name: "cinchor_permissions"})
	if got != "axiom_contract::cinchor_permissions::" {
		t.Fatalf("export prefix = %s", got)
	}
}

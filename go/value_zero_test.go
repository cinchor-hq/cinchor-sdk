package cinchor

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	omne "github.com/OmneDAO/omne-sdks/go"
)

// TestL1_EverySignedCallSendsValueZero locks Cinchor legal build-constraint L1
// ("gate, not wallet"): every signed contract call the SDK submits MUST carry
// value == 0. Cinchor authorizes-and-records; it never moves the governed funds,
// so a signed call that carried value would turn the registry into a wallet and
// change the money-transmission characterization (see gateway/legal-assessment.md
// §1 and gateway/DESIGN.md L1). All writes funnel through CapabilityRegistry.send,
// which hardcodes big.NewInt(0); this test drives every write method through a
// mock Omne node and asserts the on-the-wire "value" field is "0" each time.
func TestL1_EverySignedCallSendsValueZero(t *testing.T) {
	var sentValues []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
			ID     int    `json:"id"`
		}
		_ = json.Unmarshal(body, &req)

		result := func(v any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": v})
		}

		switch req.Method {
		case "omne_getNonce":
			result(0)
		case "omne_sendTransaction":
			// params[0] is the wire tx; capture its value field.
			if len(req.Params) > 0 {
				if wire, ok := req.Params[0].(map[string]any); ok {
					if v, ok := wire["value"].(string); ok {
						sentValues = append(sentValues, v)
					} else {
						sentValues = append(sentValues, "<missing>")
					}
				}
			}
			result("txn_test_hash")
		case "omne_getTransactionReceipt":
			result(map[string]any{"status": "committed"})
		default:
			// reads (omne_call, omne_blockNumber): not exercised by the bare
			// Registry write methods used here.
			result(nil)
		}
	}))
	defer srv.Close()

	c := Connect(Config{
		Network:  NetworkConfig{Name: "test", ChainID: 3, RPCURL: srv.URL},
		Contract: ContractConfig{Name: "cinchor_permissions", Address: "om1zqd9x7tr2qpcsra8kns0y30nvfyxgakadeyw622dmvcmjhjf3y7hs7q8uav"},
	})

	w, err := omne.WalletFromMnemonic("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about", "")
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	signer, err := w.Account(0)
	if err != nil {
		t.Fatalf("account: %v", err)
	}

	cap := "om1z5l7h4qcdzdyxt6ddatqjaegt0nt445czjrpzfd205fegdy28m5asvpv5w9"
	cp := "om1z90lu3x766pe6w2ad4pj4qt9mzuj9as6zvx8eavuvewdyavndg4ssy2yg7z"
	const now = 1781723259

	// Drive every state-modifying method on the registry.
	if _, err := c.Registry.MintPermission(signer, cap, signer.Address, cp, 1000, now+3600, true, now, 0); err != nil {
		t.Fatalf("MintPermission: %v", err)
	}
	if _, err := c.Registry.AddAllowedCounterparty(signer, cap, cp, now, 0); err != nil {
		t.Fatalf("AddAllowedCounterparty: %v", err)
	}
	if _, err := c.Registry.UpdatePolicy(signer, cap, 2000, now+7200, now, 0); err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}
	if _, err := c.Registry.RecordAction(signer, cap, 100, cp, now, cap, 0); err != nil {
		t.Fatalf("RecordAction: %v", err)
	}
	if _, err := c.Registry.RecordAttestation(signer, cap, cap, cap, Verdict(1), now, 0); err != nil {
		t.Fatalf("RecordAttestation: %v", err)
	}
	if _, err := c.Registry.RevokePermission(signer, cap, now, 0); err != nil {
		t.Fatalf("RevokePermission: %v", err)
	}

	if len(sentValues) == 0 {
		t.Fatal("L1: no signed transactions were captured — test is not exercising the send path")
	}
	for i, v := range sentValues {
		if v != "0" {
			t.Errorf("L1 VIOLATED: signed tx #%d sent value=%q, want \"0\" (gate-not-wallet)", i, v)
		}
	}
	t.Logf("L1 holds: %d signed contract calls, all value=0", len(sentValues))
}

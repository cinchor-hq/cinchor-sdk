// Cinchor Go SDK — end-to-end example + smoke test.
//
// Drives the full lifecycle against a live Omne node + deployed contract:
//
//	mint capability → enforce (in-bounds ×2) → enforce (over-budget, refused)
//	→ attest decision → verify (tamper-evident) → revoke → enforce (refused).
//
// Every step asserts; the process exits non-zero on any mismatch.
//
//	go run . <rpc_url> <contract_address> <wallets_json>
package main

import (
	"encoding/json"
	"fmt"
	"os"

	cinchor "github.com/cinchor-hq/cinchor-sdk/go"
	omne "github.com/OmneDAO/omne-sdks/go"
)

const chainID = 3

var passed, failed int

func check(label string, cond bool, detail string) {
	mark := "PASS"
	if cond {
		passed++
	} else {
		mark = "FAIL"
		failed++
	}
	fmt.Printf("  [%s] %s  (%s)\n", mark, label, detail)
}

func main() {
	if len(os.Args) < 4 {
		fmt.Println("usage: go run . <rpc> <contract> <wallets.json>")
		os.Exit(2)
	}
	rpc, contract, walletsPath := os.Args[1], os.Args[2], os.Args[3]

	raw, err := os.ReadFile(walletsPath)
	if err != nil {
		fmt.Println("read wallets:", err)
		os.Exit(1)
	}
	var w struct {
		Principal struct {
			Mnemonic string `json:"mnemonic"`
		} `json:"principal"`
		Agent struct {
			Mnemonic string `json:"mnemonic"`
		} `json:"agent"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		fmt.Println("parse wallets:", err)
		os.Exit(1)
	}
	pw, _ := omne.WalletFromMnemonic(w.Principal.Mnemonic, "")
	aw, _ := omne.WalletFromMnemonic(w.Agent.Mnemonic, "")
	principal, _ := pw.Account(0)
	agent, _ := aw.Account(0)

	c := cinchor.Connect(cinchor.Config{
		Network:  cinchor.NetworkConfig{Name: "ignis", ChainID: chainID, RPCURL: rpc},
		Contract: cinchor.ContractConfig{Name: "cinchor_permissions", Address: contract},
	})

	fmt.Printf("\nCinchor Go smoke\n  contract: cinchor_permissions @ %s\n", contract)
	fmt.Printf("  principal: %s\n  agent:     %s\n\n", principal.Address, agent.Address)

	must := func(label string, err error) {
		if err != nil {
			fmt.Printf("  [FAIL] %s: %v\n", label, err)
			os.Exit(1)
		}
	}

	fmt.Println("1. mintCapability")
	mint, err := c.MintCapability(cinchor.MintOptions{Principal: principal, Agent: agent.Address, MaxSpend: 100, TTLSeconds: 3600})
	must("mint", err)
	cap := mint.CapabilityID
	state, err := c.GetCapability(cap)
	must("getCapability", err)
	check("capability active after mint", state.Status == cinchor.StatusActive, state.StatusLabel)
	check("maxSpend recorded", state.MaxSpend == 100, fmt.Sprintf("maxSpend=%d", state.MaxSpend))
	fmt.Printf("     capabilityId: %s\n", cap)

	fmt.Println("2. enforce 40 (in bounds)")
	a1, err := c.Enforce(cinchor.EnforceOptions{Capability: cap, Agent: agent, Amount: 40})
	must("enforce1", err)
	check("first in-bounds action allowed", a1.Allowed, a1.Reason)

	fmt.Println("3. enforce 40 (in bounds, total 80/100)")
	a2, err := c.Enforce(cinchor.EnforceOptions{Capability: cap, Agent: agent, Amount: 40})
	must("enforce2", err)
	check("second in-bounds action allowed", a2.Allowed, a2.Reason)

	fmt.Println("4. enforce 50 (over budget → refused)")
	a3, err := c.Enforce(cinchor.EnforceOptions{Capability: cap, Agent: agent, Amount: 50})
	must("enforce3", err)
	check("over-budget action refused", !a3.Allowed, a3.Reason)
	check("refusal reason is over_budget", a3.Code == cinchor.CodeOverBudget, a3.Reason)
	after, err := c.GetCapability(cap)
	must("getCapability after", err)
	check("totalSpent = 80 (refusal did not mutate)", after.TotalSpent == 80, fmt.Sprintf("totalSpent=%d", after.TotalSpent))
	check("actionCount = 2", after.ActionCount == 2, fmt.Sprintf("actionCount=%d", after.ActionCount))

	fmt.Println("5. attest a decision + verify (tamper-evidence)")
	at := cinchor.NowSecs()
	decision := map[string]any{"model": "demo-triage-v1", "input": map[string]any{"claim": "A-123"}, "output": "approve", "at": at}
	att, err := c.Attest(cinchor.AttestOptions{Capability: cap, Agent: agent, Context: decision, Verdict: cinchor.VerdictInPolicy})
	must("attest", err)
	good, err := c.VerifyAttestation(decision, att.AttestationID)
	must("verify", err)
	check("attestation verifies against the original context", good.OK, fmt.Sprintf("hash=%.14s…", att.ContextHash))
	tampered := map[string]any{"model": "demo-triage-v1", "input": map[string]any{"claim": "A-123"}, "output": "deny", "at": at}
	bad, err := c.VerifyAttestation(tampered, att.AttestationID)
	must("verify tampered", err)
	check("tampered context fails verification", !bad.OK, "hash mismatch detected")

	fmt.Println("6. revoke + enforce (refused: revoked)")
	_, err = c.Revoke(cap, principal, 0, 0)
	must("revoke", err)
	revoked, err := c.GetCapability(cap)
	must("getCapability revoked", err)
	check("capability revoked", revoked.Status == cinchor.StatusRevoked, revoked.StatusLabel)
	a4, err := c.Enforce(cinchor.EnforceOptions{Capability: cap, Agent: agent, Amount: 10})
	must("enforce4", err)
	check("post-revocation action refused", !a4.Allowed, a4.Reason)
	check("refusal reason is revoked", a4.Code == cinchor.CodeRevoked, a4.Reason)

	result := "PASS"
	if failed != 0 {
		result = "FAIL"
	}
	fmt.Printf("\n%s — %d checks passed, %d failed\n\n", result, passed, failed)
	if failed != 0 {
		os.Exit(1)
	}
}

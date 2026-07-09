// Package nonce provides a process-wide, per-account nonce allocator shared by
// every path that submits signed transactions from a gateway-held key (the
// mint principal, each tenant agent, and the funder treasury).
//
// Why it exists: the Omne node does not enforce per-account nonce ordering and
// dedups only by full transaction hash, but omne_getNonce returns a
// committed-only counter that advances one-per-included-tx at block cadence.
// Under those semantics two goroutines that each read the committed nonce and
// submit different-payload transactions from the SAME signer can both read the
// same value — the confirmed treasury double-funding hazard (funder re-reading
// committed and emitting same-nonce/different-payload transfers). This allocator
// serializes nonce allocation PER ADDRESS (distinct addresses stay fully
// parallel), hands out monotonically increasing nonces seeded from committed
// state, and advances only on a successful mempool-accept — which also fixes the
// base SDK's advance-before-send gap bug. It holds its lock only across
// sign+submit, never across receipt-waiting, so it adds no latency.
package nonce

import "sync"

// chainNoncer is the read side of the chain this allocator seeds from.
// *omne.OmneClient satisfies it via GetNonce.
type chainNoncer interface {
	GetNonce(address string) (uint64, error)
}

type acct struct {
	mu     sync.Mutex
	seeded bool
	next   uint64
}

// Manager is the single shared nonce authority. Safe for concurrent use.
type Manager struct {
	chain chainNoncer
	mu    sync.Mutex // guards accts map lookup/creation only
	accts map[string]*acct
}

// NewManager builds a Manager seeded lazily from chain (the same OmneClient the
// gateway already holds; committed nonce is global chain state, so the specific
// client instance does not matter).
func NewManager(chain chainNoncer) *Manager {
	return &Manager{chain: chain, accts: make(map[string]*acct)}
}

func (m *Manager) acctFor(addr string) *acct {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.accts[addr]
	if a == nil {
		a = &acct{}
		m.accts[addr] = a
	}
	return a
}

// Submit serializes nonce allocation for addr and invokes send with the nonce
// to use. The send closure must build+sign+submit a transaction at exactly that
// nonce and return the tx hash (mempool-accept), NOT wait for the receipt —
// receipt-waiting belongs to the caller, outside the held lock. The local
// counter advances only when send succeeds, so a failed submit leaves the nonce
// unconsumed for the next call (no gap). On a (future) nonce-conflict error the
// counter jumps forward to committed and the caller may retry with an identical
// payload — identical hash, safe under the node's hash-dedup.
func (m *Manager) Submit(addr string, send func(nonce uint64) (string, error)) (string, error) {
	a := m.acctFor(addr)
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.seeded {
		if err := m.reseed(a, addr); err != nil {
			return "", err
		}
	}
	hash, err := send(a.next)
	if err != nil {
		if isNonceError(err) {
			_ = m.reseed(a, addr) // jump forward to committed; caller retries
		}
		return "", err
	}
	a.next++
	return hash, nil
}

// reseed adopts the committed nonce, but only ever moves the counter FORWARD:
// committed lags pending under load, so regressing below a nonce already handed
// out would reuse it. Callers hold a.mu.
func (m *Manager) reseed(a *acct, addr string) error {
	committed, err := m.chain.GetNonce(addr)
	if err != nil {
		return err
	}
	if committed > a.next {
		a.next = committed
	}
	a.seeded = true
	return nil
}

// isNonceError reports whether err is the node signalling a nonce conflict.
// Dormant seam: the current node enforces no per-account nonce ordering and
// emits no such error, so this returns false today. It is the single point to
// update — with the confirmed error code/message — the day the node begins
// rejecting non-sequential nonces. Kept conservative so transient RPC errors
// never thrash the seed.
func isNonceError(err error) bool {
	return false
}

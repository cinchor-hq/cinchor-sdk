package nonce

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeChain is a controllable chainNoncer.
type fakeChain struct {
	mu       sync.Mutex
	nonce    uint64
	calls    int32
	errFirst int32 // if >0, return an error on the first N GetNonce calls
}

func (f *fakeChain) GetNonce(addr string) (uint64, error) {
	atomic.AddInt32(&f.calls, 1)
	if atomic.LoadInt32(&f.errFirst) > 0 {
		atomic.AddInt32(&f.errFirst, -1)
		return 0, errors.New("rpc down")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nonce, nil
}

func TestSequentialSameAddress(t *testing.T) {
	m := NewManager(&fakeChain{nonce: 0})
	var got []uint64
	for i := 0; i < 5; i++ {
		_, err := m.Submit("A", func(n uint64) (string, error) { got = append(got, n); return "tx", nil })
		if err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}
	want := []uint64{0, 1, 2, 3, 4}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("nonce[%d]=%d want %d (%v)", i, got[i], want[i], got)
		}
	}
}

func TestCrossAddressIndependence(t *testing.T) {
	m := NewManager(&fakeChain{nonce: 100})
	var a, b uint64
	m.Submit("A", func(n uint64) (string, error) { a = n; return "tx", nil })
	m.Submit("B", func(n uint64) (string, error) { b = n; return "tx", nil })
	m.Submit("A", func(n uint64) (string, error) { a = n; return "tx", nil })
	// Both seed from committed 100 independently; A advanced once more.
	if a != 101 || b != 100 {
		t.Fatalf("A=%d (want 101), B=%d (want 100)", a, b)
	}
}

func TestAdvanceOnlyOnSuccess(t *testing.T) {
	m := NewManager(&fakeChain{nonce: 7})
	// A failing send must NOT consume the nonce.
	var seen []uint64
	_, err := m.Submit("A", func(n uint64) (string, error) { seen = append(seen, n); return "", errors.New("submit failed") })
	if err == nil {
		t.Fatal("expected error")
	}
	_, _ = m.Submit("A", func(n uint64) (string, error) { seen = append(seen, n); return "tx", nil })
	if len(seen) != 2 || seen[0] != 7 || seen[1] != 7 {
		t.Fatalf("nonce not reused after failure: %v (want [7 7])", seen)
	}
}

func TestMonotonicReseedNeverRegresses(t *testing.T) {
	fc := &fakeChain{nonce: 5}
	m := NewManager(fc)
	m.Submit("A", func(n uint64) (string, error) { return "tx", nil }) // seed 5 -> next 6
	// Committed regresses (stale/lagging read); reseed must NOT pull us back.
	fc.mu.Lock()
	fc.nonce = 3
	fc.mu.Unlock()
	a := m.acctFor("A")
	m.reseed(a, "A")
	if a.next != 6 {
		t.Fatalf("reseed regressed to %d, want 6", a.next)
	}
	// Committed jumps ahead of us; reseed must jump forward.
	fc.mu.Lock()
	fc.nonce = 20
	fc.mu.Unlock()
	m.reseed(a, "A")
	if a.next != 20 {
		t.Fatalf("reseed did not jump forward: %d, want 20", a.next)
	}
}

func TestRestartReseedsFromCommitted(t *testing.T) {
	// A fresh Manager (cold start) seeds strictly from committed state.
	m := NewManager(&fakeChain{nonce: 42})
	var first uint64
	m.Submit("A", func(n uint64) (string, error) { first = n; return "tx", nil })
	if first != 42 {
		t.Fatalf("cold-start nonce=%d, want 42", first)
	}
}

func TestSeedErrorPropagates(t *testing.T) {
	m := NewManager(&fakeChain{nonce: 1, errFirst: 1})
	sent := false
	_, err := m.Submit("A", func(n uint64) (string, error) { sent = true; return "tx", nil })
	if err == nil {
		t.Fatal("expected seed error to propagate")
	}
	if sent {
		t.Fatal("send must not run when seeding failed")
	}
}

// TestConcurrentDistinctNonces is the core safety proof: run with -race.
func TestConcurrentDistinctNonces(t *testing.T) {
	const N = 200
	m := NewManager(&fakeChain{nonce: 0})
	var mu sync.Mutex
	seen := make(map[uint64]int)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Submit("A", func(n uint64) (string, error) {
				mu.Lock()
				seen[n]++
				mu.Unlock()
				return "tx", nil
			})
		}()
	}
	wg.Wait()
	if len(seen) != N {
		t.Fatalf("got %d distinct nonces, want %d (collisions present)", len(seen), N)
	}
	for n, c := range seen {
		if c != 1 {
			t.Fatalf("nonce %d handed out %d times", n, c)
		}
	}
	// Strictly 0..N-1, no gaps.
	for i := uint64(0); i < N; i++ {
		if seen[i] != 1 {
			t.Fatalf("missing nonce %d in 0..%d", i, N-1)
		}
	}
}

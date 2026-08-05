package tlsutil

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"
)

// fakeStore is a mutex-guarded stand-in for config.Config's synchronized
// LookupTrusted/SetTrusted methods, used to prove VerifyFunc is race-free
// when its lookup/onNew callbacks are themselves synchronized.
type fakeStore struct {
	mu sync.Mutex
	m  map[string]string
}

func newFakeStore() *fakeStore { return &fakeStore{m: map[string]string{}} }

func (s *fakeStore) lookup(ip string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.m[ip]
	return fp, ok
}

func (s *fakeStore) set(ip, fp string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[ip] = fp
}

func selfSignedDER(t *testing.T) []byte {
	t.Helper()
	tmp := t.TempDir()
	cert, _, err := generate(tmp+"/cert.pem", tmp+"/key.pem")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return cert.Certificate[0]
}

func TestVerifyFuncTOFU(t *testing.T) {
	der := selfSignedDER(t)
	store := newFakeStore()

	var onNewCalls int
	verify := VerifyFunc("10.0.0.5", store.lookup, func(ip, fp string) {
		onNewCalls++
		store.set(ip, fp)
	})

	// First contact: trusted and recorded.
	if err := verify([][]byte{der}, nil); err != nil {
		t.Fatalf("first contact: unexpected error: %v", err)
	}
	if onNewCalls != 1 {
		t.Fatalf("expected onNew called once, got %d", onNewCalls)
	}

	// Subsequent contact with same cert: no error, no re-trust.
	if err := verify([][]byte{der}, nil); err != nil {
		t.Fatalf("second contact same cert: unexpected error: %v", err)
	}
	if onNewCalls != 1 {
		t.Fatalf("onNew should not fire again on a match, got %d calls", onNewCalls)
	}

	// A different cert from the same IP must be rejected (mismatch).
	other := selfSignedDER(t)
	if err := verify([][]byte{other}, nil); err == nil {
		t.Fatal("expected fingerprint mismatch error, got nil")
	}
}

func TestVerifyFuncNoCerts(t *testing.T) {
	store := newFakeStore()
	verify := VerifyFunc("10.0.0.5", store.lookup, func(string, string) {})
	if err := verify(nil, nil); err == nil {
		t.Fatal("expected error for empty rawCerts")
	}
}

// TestVerifyFuncConcurrentHandshakes reproduces the scenario that caused
// the original bug: multiple simultaneous TLS handshakes to the same
// not-yet-trusted peer (e.g. a multi-file send with upload_concurrency>1).
// Run with -race: this must not report a data race, and the store must end
// up with a single consistent fingerprint for the peer.
func TestVerifyFuncConcurrentHandshakes(t *testing.T) {
	der := selfSignedDER(t)
	store := newFakeStore()

	verify := VerifyFunc("10.0.0.9", store.lookup, store.set)

	const n = 32
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- verify([][]byte{der}, nil)
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent handshake failed: %v", err)
		}
	}

	fp, ok := store.lookup("10.0.0.9")
	if !ok {
		t.Fatal("peer not trusted after concurrent handshakes")
	}
	sum := sha256.Sum256(der)
	want := "sha256:" + hex.EncodeToString(sum[:])
	if fp != want {
		t.Fatalf("unexpected fingerprint: got %s want %s", fp, want)
	}
}

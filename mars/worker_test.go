package mars

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type fakeClock struct{ nanos atomic.Int64 }

func newClock() *fakeClock {
	c := &fakeClock{}
	c.nanos.Store(time.Now().UnixNano())
	return c
}
func (c *fakeClock) now() time.Time          { return time.Unix(0, c.nanos.Load()) }
func (c *fakeClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }

type fakeWorkerStore struct {
	mu        sync.Mutex
	next      clientv3.LeaseID
	keys      map[string]clientv3.LeaseID
	leases    map[clientv3.LeaseID]bool
	claimErr  error
	renewErr  error
	renewHook func()
	ttl       int64
}

func newStore() *fakeWorkerStore {
	return &fakeWorkerStore{keys: make(map[string]clientv3.LeaseID), leases: make(map[clientv3.LeaseID]bool), ttl: 10}
}
func (s *fakeWorkerStore) grant(_ context.Context, _ int64) (clientv3.LeaseID, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	s.leases[s.next] = true
	return s.next, s.ttl, nil
}
func (s *fakeWorkerStore) claim(_ context.Context, key, _ string, id clientv3.LeaseID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimErr != nil {
		return false, s.claimErr
	}
	if _, exists := s.keys[key]; exists {
		return false, nil
	}
	s.keys[key] = id
	return true, nil
}
func (s *fakeWorkerStore) renew(_ context.Context, _ clientv3.LeaseID) (int64, error) {
	if s.renewHook != nil {
		s.renewHook()
	}
	return s.ttl, s.renewErr
}
func (s *fakeWorkerStore) owned(_ context.Context, key, _ string, id clientv3.LeaseID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keys[key] == id, nil
}
func (s *fakeWorkerStore) revoke(_ context.Context, id clientv3.LeaseID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.leases, id)
	for key, lease := range s.keys {
		if lease == id {
			delete(s.keys, key)
		}
	}
	return nil
}

func testWorker(t *testing.T) (*workerLease, *fakeWorkerStore, *fakeClock) {
	t.Helper()
	s, c := newStore(), newClock()
	w, err := reserveWorker(context.Background(), s, "test/mars/worker", "node", 10, c.now)
	if err != nil {
		t.Fatal(err)
	}
	c.advance(time.Second)
	return w, s, c
}

func TestConcurrentWorkerReservations(t *testing.T) {
	s := newStore()
	var wg sync.WaitGroup
	numbers := make(chan int64, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w, err := reserveWorker(context.Background(), s, "test/mars/worker", fmt.Sprint(i), 10, time.Now)
			if err == nil {
				numbers <- w.number
			}
		}(i)
	}
	wg.Wait()
	close(numbers)
	seen := make(map[int64]bool)
	for number := range numbers {
		if number < 0 || number > 31 || seen[number] {
			t.Fatalf("invalid/repeated worker: %d", number)
		}
		seen[number] = true
	}
	if len(seen) != 32 || len(s.leases) != 32 {
		t.Fatalf("workers=%d, leases=%d", len(seen), len(s.leases))
	}
}

func TestClaimFailureRevokesUnusedLease(t *testing.T) {
	s := newStore()
	s.claimErr = errors.New("etcd unavailable")
	if _, err := reserveWorker(context.Background(), s, "test/worker", "node", 10, time.Now); err == nil {
		t.Fatal("expected error")
	}
	if len(s.leases) != 0 {
		t.Fatal("unused lease leaked")
	}
}

func TestWorkerWaitsOneSecondBeforeReusingSequence(t *testing.T) {
	w, _, c := testWorker(t)
	c.advance(-time.Second)
	if w.available() == nil {
		t.Fatal("worker active before reuse delay")
	}
	c.advance(time.Second)
	if err := w.available(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerExpiresWithoutHeartbeatEvenIfKeepaliveIsStuck(t *testing.T) {
	w, _, c := testWorker(t)
	c.advance(9 * time.Second)
	if w.available() == nil {
		t.Fatal("expired worker still active")
	}
	if w.renew(context.Background()) == nil {
		t.Fatal("expired worker revived")
	}
}

func TestRenewalFailuresFencePermanently(t *testing.T) {
	for _, mode := range []string{"rpc", "deleted", "replaced", "ttl-zero", "late-response"} {
		t.Run(mode, func(t *testing.T) {
			w, s, c := testWorker(t)
			switch mode {
			case "rpc":
				s.renewErr = errors.New("connection lost")
			case "deleted":
				delete(s.keys, w.key)
			case "replaced":
				s.keys[w.key] = w.id + 1
			case "ttl-zero":
				s.ttl = 0
			case "late-response":
				s.renewHook = func() { c.advance(10 * time.Second) }
			}
			if w.renew(context.Background()) == nil || w.available() == nil {
				t.Fatal("lost lease still active")
			}
			s.renewErr, s.renewHook, s.ttl = nil, nil, 10
			s.keys[w.key] = w.id
			if w.renew(context.Background()) == nil {
				t.Fatal("fenced generator revived")
			}
		})
	}
}

func TestRenewalDeadlineStartsBeforeRPC(t *testing.T) {
	w, s, c := testWorker(t)
	started := c.now()
	s.renewHook = func() { c.advance(2 * time.Second) }
	if err := w.renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !w.deadline.Equal(started.Add(10 * time.Second)) {
		t.Fatal("network delay extended deadline")
	}
}

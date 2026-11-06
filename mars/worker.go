package mars

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Both ID formats share a worker number. The 53-bit format has only 5 bits.
const maxWorkerID = 31

const workerRequestTimeout = 2 * time.Second

var errWorkerUnavailable = errors.New("worker lease unavailable; restart required")

type idGenerator interface {
	GetStr() string
	GetStr53() string
	GetTime(int64) string
	GetStep(int64) int64
	GetNode(int64) int64
}

type workerStore interface {
	grant(context.Context, int64) (clientv3.LeaseID, int64, error)
	claim(context.Context, string, string, clientv3.LeaseID) (bool, error)
	renew(context.Context, clientv3.LeaseID) (int64, error)
	owned(context.Context, string, string, clientv3.LeaseID) (bool, error)
	revoke(context.Context, clientv3.LeaseID) error
}

type etcdWorkerStore struct{ client *clientv3.Client }

func (s etcdWorkerStore) grant(ctx context.Context, ttl int64) (clientv3.LeaseID, int64, error) {
	r, err := s.client.Grant(ctx, ttl)
	if err != nil {
		return 0, 0, err
	}
	return r.ID, r.TTL, nil
}

func (s etcdWorkerStore) claim(ctx context.Context, key, owner string, lease clientv3.LeaseID) (bool, error) {
	r, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, owner, clientv3.WithLease(lease))).Commit()
	if err != nil {
		return false, err
	}
	return r.Succeeded, nil
}

func (s etcdWorkerStore) renew(ctx context.Context, lease clientv3.LeaseID) (int64, error) {
	r, err := s.client.KeepAliveOnce(ctx, lease)
	if err != nil {
		return 0, err
	}
	return r.TTL, nil
}

func (s etcdWorkerStore) owned(ctx context.Context, key, owner string, lease clientv3.LeaseID) (bool, error) {
	r, err := s.client.Get(ctx, key)
	if err != nil {
		return false, err
	}
	return len(r.Kvs) == 1 && r.Kvs[0].Lease == int64(lease) && string(r.Kvs[0].Value) == owner, nil
}

func (s etcdWorkerStore) revoke(ctx context.Context, lease clientv3.LeaseID) error {
	_, err := s.client.Revoke(ctx, lease)
	return err
}

type workerLease struct {
	store    workerStore
	id       clientv3.LeaseID
	key      string
	owner    string
	number   int64
	now      func() time.Time
	mu       sync.RWMutex
	deadline time.Time
	readyAt  time.Time
	failed   bool
}

func reserveWorker(ctx context.Context, store workerStore, prefix, owner string, ttl int64, now func() time.Time) (*workerLease, error) {
	// Start the local deadline before the RPC, so network delay cannot extend it.
	started := now()
	id, grantedTTL, err := store.grant(ctx, ttl)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			cleanup, cancel := context.WithTimeout(context.Background(), workerRequestTimeout)
			defer cancel()
			_ = store.revoke(cleanup, id)
		}
	}()
	for num := int64(0); num <= maxWorkerID; num++ {
		key := prefix + "/" + strconv.FormatInt(num, 10)
		claimed, err := store.claim(ctx, key, owner, id)
		if err != nil {
			return nil, err
		}
		if !claimed {
			continue
		}
		w := &workerLease{
			store: store, id: id, key: key, owner: owner, number: num, now: now,
			deadline: started.Add(time.Duration(grantedTTL) * time.Second),
			// A reused worker must cross a whole 53-bit timestamp tick (one second)
			// before its sequence can restart at zero.
			readyAt: now().Add(time.Second),
		}
		if !now().Before(w.deadline) || grantedTTL <= 0 {
			return nil, errWorkerUnavailable
		}
		success = true
		return w, nil
	}
	return nil, fmt.Errorf("all %d worker numbers are occupied", maxWorkerID+1)
}

func (w *workerLease) available() error {
	if w == nil {
		return errWorkerUnavailable
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	now := w.now()
	if w.failed || !now.Before(w.deadline) || now.Before(w.readyAt) {
		return errWorkerUnavailable
	}
	return nil
}

func (w *workerLease) fence() {
	w.mu.Lock()
	w.failed = true
	w.mu.Unlock()
}

func (w *workerLease) renew(ctx context.Context) error {
	started := w.now()
	w.mu.RLock()
	valid := !w.failed && started.Before(w.deadline)
	w.mu.RUnlock()
	if !valid {
		w.fence()
		return errWorkerUnavailable
	}
	ttl, err := w.store.renew(ctx, w.id)
	if err != nil {
		w.fence()
		return err
	}
	owned, err := w.store.owned(ctx, w.key, w.owner, w.id)
	if err != nil || !owned || ttl <= 0 {
		w.fence()
		return errWorkerUnavailable
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	// Never revive an expired/fenced generator, even after a delayed success.
	deadline := started.Add(time.Duration(ttl) * time.Second)
	if w.failed || !w.now().Before(w.deadline) || !w.now().Before(deadline) {
		w.failed = true
		return errWorkerUnavailable
	}
	w.deadline = deadline
	return nil
}

func (m *mars) keepWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(m.etcdKeyTimeOut) * time.Second / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.worker.fence()
			return
		case <-ticker.C:
			request, cancel := context.WithTimeout(ctx, workerRequestTimeout)
			err := m.worker.renew(request)
			cancel()
			if err != nil {
				m.log.Errorf("worker %d fenced; restart required: %v", m.workerNum, err)
				return
			}
		}
	}
}

// Build the whole response before writing it. A lease lost during generation
// must not produce a successful response or a partially written ID batch.
func (m *mars) generateIDs(count int, short bool) ([]string, error) {
	if err := m.worker.available(); err != nil {
		return nil, err
	}
	ids := make([]string, count)
	for i := range ids {
		if short {
			ids[i] = m.gen.GetStr53()
		} else {
			ids[i] = m.gen.GetStr()
		}
	}
	if err := m.worker.available(); err != nil {
		return nil, err
	}
	return ids, nil
}

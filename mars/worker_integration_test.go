package mars

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// This test starts a disposable local etcd; it never uses an existing cluster.
func localEtcd(t *testing.T) *clientv3.Client {
	t.Helper()
	binary := os.Getenv("MARS_TEST_ETCD_BINARY")
	if binary == "" {
		t.Skip("set MARS_TEST_ETCD_BINARY to run real etcd transaction/lease tests")
	}
	address := func() string {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		return "http://" + listener.Addr().String()
	}
	clientURL, peerURL := address(), address()
	dir := t.TempDir()
	logFile, err := os.Create(filepath.Join(dir, "etcd.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "--name=test", "--data-dir="+filepath.Join(dir, "data"),
		"--listen-client-urls="+clientURL, "--advertise-client-urls="+clientURL,
		"--listen-peer-urls="+peerURL, "--initial-advertise-peer-urls="+peerURL,
		"--initial-cluster=test="+peerURL, "--logger=zap", "--log-level=error")
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = logFile.Close() })
	client, err := clientv3.New(clientv3.Config{Endpoints: []string{clientURL}, DialTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err := client.Status(ctx, clientURL)
		cancel()
		if err == nil {
			return client
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("local etcd did not become ready")
	return nil
}

func TestRealEtcdConcurrentReservationsAndFencing(t *testing.T) {
	client := localEtcd(t)
	store := etcdWorkerStore{client}
	var wg sync.WaitGroup
	workers := make(chan *workerLease, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			w, err := reserveWorker(ctx, store, "regression/mars/worker", fmt.Sprint(i), 10, time.Now)
			if err == nil {
				workers <- w
			}
		}(i)
	}
	wg.Wait()
	close(workers)
	seen := make(map[int64]bool)
	var victim *workerLease
	for w := range workers {
		if seen[w.number] {
			t.Fatalf("duplicate worker %d", w.number)
		}
		seen[w.number] = true
		victim = w
	}
	if len(seen) != 32 {
		t.Fatalf("reserved %d workers, want 32", len(seen))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := victim.renew(ctx); err != nil {
		t.Fatal(err)
	}
	// An overwritten key with the same address but another lease must fence.
	other, err := client.Grant(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Put(ctx, victim.key, victim.owner, clientv3.WithLease(other.ID)); err != nil {
		t.Fatal(err)
	}
	if victim.renew(ctx) == nil {
		t.Fatal("worker accepted another lease's key")
	}
	if _, err := client.Put(ctx, victim.key, victim.owner, clientv3.WithLease(victim.id)); err != nil {
		t.Fatal(err)
	}
	if victim.renew(ctx) == nil {
		t.Fatal("fenced worker revived after key restoration")
	}
	if err := store.revoke(ctx, victim.id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.renew(ctx, victim.id); err == nil {
		t.Fatal("revoked lease renewed")
	}
}

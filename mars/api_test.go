package mars

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Fengxq2014/mars/generator"
	"github.com/bsm/redeo"
	"github.com/go-redis/redis"
)

type testGenerator struct {
	next atomic.Int64
	hook func()
}

func (g *testGenerator) GetStr() string {
	if g.hook != nil {
		g.hook()
	}
	return fmt.Sprint(g.next.Add(1))
}
func (g *testGenerator) GetStr53() string   { return g.GetStr() }
func (*testGenerator) GetTime(int64) string { return "" }
func (*testGenerator) GetStep(int64) int64  { return 0 }
func (*testGenerator) GetNode(int64) int64  { return 0 }

func TestHTTPRejectsExpiredWorkerWithoutPartialBatch(t *testing.T) {
	previous := m
	defer func() { m = previous }()
	for _, short := range []bool{false, true} {
		for _, count := range []string{"", "?num=3"} {
			w, _, _ := testWorker(t)
			g := &testGenerator{hook: w.fence}
			m = &mars{worker: w, gen: g}
			path, handler := "/id", GetID
			if short {
				path, handler = "/id53", GetID53
			}
			response := httptest.NewRecorder()
			handler(response, httptest.NewRequest(http.MethodGet, path+count, nil))
			if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "1\n") {
				t.Fatalf("partial successful response: %d %q", response.Code, response.Body.String())
			}
			before := g.next.Load()
			handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
			if g.next.Load() != before {
				t.Fatal("fenced worker generated again")
			}
		}
	}
}

func TestHTTPResponseFormatAndCountValidation(t *testing.T) {
	previous := m
	defer func() { m = previous }()
	w, _, _ := testWorker(t)
	m = &mars{worker: w, gen: &testGenerator{}}
	for _, handler := range []http.HandlerFunc{GetID, GetID53} {
		for _, tc := range []struct {
			query       string
			code, lines int
		}{
			{"", 200, 1}, {"?num=3", 200, 3}, {"?num=0", 400, 0},
			{"?num=-1", 400, 0}, {"?num=101", 400, 0}, {"?num=x", 400, 0},
		} {
			r := httptest.NewRecorder()
			handler(r, httptest.NewRequest("GET", "/id"+tc.query, nil))
			if r.Code != tc.code {
				t.Fatalf("%s: status %d", tc.query, r.Code)
			}
			if tc.code == 200 && len(strings.Fields(r.Body.String())) != tc.lines {
				t.Fatal("wrong batch length")
			}
		}
	}
}

func TestRedisRejectsLeaseLossForBothFormatsAndBatchSizes(t *testing.T) {
	w, _, _ := testWorker(t)
	var fenceOnGenerate atomic.Bool
	g := &testGenerator{hook: func() {
		if fenceOnGenerate.Load() {
			w.fence()
		}
	}}
	server := &mars{worker: w, gen: g, redisSrv: redeo.NewServer(nil), redisPasswd: "test-only"}
	server.initRedisSrv()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go server.redisSrv.Serve(listener)
	client := redis.NewClient(&redis.Options{Addr: listener.Addr().String(), Password: "test-only"})
	defer client.Close()
	if id, err := client.Get("id").Result(); err != nil || id == "" {
		t.Fatal(err)
	}
	fenceOnGenerate.Store(true)
	for _, key := range []string{"id", "id53"} {
		if _, err := client.Get(key).Result(); err == nil || !strings.Contains(err.Error(), "worker lease unavailable") {
			t.Fatalf("GET %s: %v", key, err)
		}
		if ids, err := client.LRange(key, 0, 3).Result(); err == nil || len(ids) != 0 {
			t.Fatalf("LRANGE %s: %v %v", key, ids, err)
		}
	}
}

func TestConcurrentIDUniquenessWithValidWorker(t *testing.T) {
	for _, short := range []bool{false, true} {
		w, _, _ := testWorker(t)
		server := &mars{worker: w, gen: generator.New(0)}
		var wg sync.WaitGroup
		ids := make(chan []string, 16)
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				batch, err := server.generateIDs(100, short)
				if err != nil {
					t.Error(err)
				}
				ids <- batch
			}()
		}
		wg.Wait()
		close(ids)
		seen := make(map[string]bool)
		for batch := range ids {
			for _, id := range batch {
				if seen[id] {
					t.Fatalf("duplicate ID: %s", id)
				}
				seen[id] = true
			}
		}
		if len(seen) != 1600 {
			t.Fatalf("received %d IDs", len(seen))
		}
	}
}

func TestGenerationCrossingLocalLeaseDeadlineIsDiscarded(t *testing.T) {
	w, _, clock := testWorker(t)
	server := &mars{worker: w, gen: &testGenerator{hook: func() { clock.advance(10 * time.Second) }}}
	if ids, err := server.generateIDs(1, false); err == nil || ids != nil {
		t.Fatal("returned ID after expiry")
	}
}

func TestConcurrentRenewalGenerationAndFencing(t *testing.T) {
	w, _, _ := testWorker(t)
	server := &mars{worker: w, gen: generator.New(0)}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if _, err := server.generateIDs(10, false); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if err := w.renew(context.Background()); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
	w.fence()
	if ids, err := server.generateIDs(1, false); err == nil || ids != nil {
		t.Fatal("fenced worker generated again")
	}
}

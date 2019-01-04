package mars

import (
	"context"
	"fmt"
	"github.com/Fengxq2014/mars/etcdsrv"
	"github.com/Fengxq2014/mars/generator"
	"github.com/bsm/redeo"
	"github.com/bsm/redeo/resp"
	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/clientv3/clientv3util"
	"github.com/coreos/etcd/clientv3/concurrency"
	"github.com/coreos/etcd/mvcc/mvccpb"
	"github.com/go-redis/redis"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type mars struct {
	httpAddr       string
	httpSrv        *http.Server
	redisSrv       *redeo.Server
	tcpAddr        string
	log            *log.Logger
	leader         bool
	leaderAddr     string
	etcdCli        *clientv3.Client
	mu             sync.RWMutex
	gen            *generator.Generator
	keepChan       chan bool
	campChan       chan bool
	ticker         *time.Ticker
	etcdKeyTimeOut int64
	workerNum      int64
	redisPasswd    string
	httpName       string
	httpPasswd     string
}

type MarsConfig struct {
	HttpAddr      string
	TcpAddr       string
	Log           *log.Logger
	EtcdEndPoints string
	RedisPasswd   string
	HttpName      string
	HttpPasswd    string
}

var (
	campaignKey = "mars/campaign"
	leaderKey   = "mars/node/leader"
	nodeKey     = "mars/node"
	workerKey   = "mars/worker"
	version     = "1.0.0"
	TxnFailed   = errors.New("etcd txn failed")
	once        sync.Once
	m           *mars
)

func New(cfg *MarsConfig) *mars {
	once.Do(func() {
		if cfg == nil {
			cfg = &MarsConfig{
				HttpAddr:      ":8080",
				TcpAddr:       ":8089",
				Log:           log.New(),
				EtcdEndPoints: "localhost:23790",
			}
		}
		r := mux.NewRouter()
		amw := authenticationMiddleware{}
		amw.Populate(cfg.HttpName, cfg.HttpPasswd)
		r.Use(amw.Middleware)

		r.HandleFunc("/id", GetID).Methods("GET")
		r.HandleFunc("/info/{id:[0-9]+}", GetIDInfo).Methods("GET")
		s := &http.Server{
			Addr:           cfg.HttpAddr,
			Handler:        r,
			ReadTimeout:    10 * time.Second,
			WriteTimeout:   10 * time.Second,
			MaxHeaderBytes: 1 << 20,
		}
		m = &mars{
			httpAddr:       cfg.HttpAddr,
			tcpAddr:        cfg.TcpAddr,
			httpSrv:        s,
			redisSrv:       redeo.NewServer(nil),
			etcdCli:        etcdsrv.New(cfg.EtcdEndPoints).Cli,
			log:            cfg.Log,
			keepChan:       make(chan bool, 1),
			campChan:       make(chan bool, 1),
			ticker:         time.NewTicker(time.Second * 7),
			etcdKeyTimeOut: 10,
			redisPasswd:    cfg.RedisPasswd,
			httpName:       cfg.HttpName,
			httpPasswd:     cfg.HttpPasswd,
		}
		fmt.Printf(`

  _ __ ___   __ _ _ __ ___ 
 | '_ `+"`"+` _ \ / _`+"`"+` | '__/ __|
 | | | | | | (_| | |  \__ \
 |_| |_| |_|\__,_|_|  |___/
                           
          
 version: %s

`, version)
		m.initRedisSrv()
		err := m.initialize()
		if err != nil {
			panic(err)
		}
		go m.chanFuc()
	})
	return m
}

func (m *mars) StartHttp() {
	httpLog := m.log.WithField("module", "http")
	go func() {
		httpLog.Printf("http server start listen on:%s", m.httpAddr)
		httpLog.Fatal(m.httpSrv.ListenAndServe())
		httpLog.Println("http server shutdown")
	}()
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	httpLog.Println(<-ch)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.etcdCli.Close()
	if err := m.httpSrv.Shutdown(ctx); err != nil {
		httpLog.Fatalf("http server Shutdown err:%v", err)
	}
	httpLog.Println("done.")
}

func (m *mars) StartTcp() {
	m.initRedisSrv()
	m.log.Infof("tcp server start listen on:" + m.tcpAddr)
	go func() {
		lis, err := net.Listen("tcp", m.tcpAddr)
		if err != nil {
			panic(err)
		}
		defer lis.Close()
		err = m.redisSrv.Serve(lis)
		if err != nil {
			m.log.Error(err)
		}
	}()
}

func (m *mars) initRedisSrv() {
	type ctxAuthOK struct{}
	broker := redeo.NewPubSubBroker()
	m.redisSrv.HandleFunc("ping", func(w resp.ResponseWriter, c *resp.Command) {
		cli := redeo.GetClient(c.Context())
		value := cli.Context().Value(ctxAuthOK{})
		b, ok := value.(bool)
		if ok && b {
			w.AppendInlineString("PONG")
		} else {
			w.AppendError("NOAUTH Authentication required.")
		}
	})
	m.redisSrv.HandleFunc("info", func(w resp.ResponseWriter, _ *resp.Command) {
		w.AppendBulkString(m.redisSrv.Info().String())
	})
	m.redisSrv.HandleFunc("auth", func(w resp.ResponseWriter, c *resp.Command) {
		if c.Arg(0).String() == m.redisPasswd {
			client := redeo.GetClient(c.Context())
			client.SetContext(context.WithValue(client.Context(), ctxAuthOK{}, true))
			w.AppendOK()
		} else {
			w.AppendError("NOAUTH Authentication required.")
		}
	})
	m.redisSrv.Handle("get", redeo.WrapperFunc(func(c *resp.Command) interface{} {
		cli := redeo.GetClient(c.Context())
		value := cli.Context().Value(ctxAuthOK{})
		b, ok := value.(bool)
		if ok && b {
			if c.ArgN() != 1 {
				return redeo.ErrWrongNumberOfArgs(c.Name)
			}
			if c.Arg(0).String() == "node" {
				b, _ := m.isLeader()
				if !b {
					return errors.New("ERR not leader")
				}
				num, err := m.getNodeNum()
				if err != nil {
					return err
				}
				return num
			}
			if c.Arg(0).String() == "id" {
				return m.gen.GetStr()
			}
			return nil
		} else {
			return errors.New("NOAUTH Authentication required.")
		}
	}))
	m.redisSrv.HandleFunc("sentinel", func(w resp.ResponseWriter, c *resp.Command) {
		cli := redeo.GetClient(c.Context())
		value := cli.Context().Value(ctxAuthOK{})
		b, ok := value.(bool)
		if ok && b {
			if c.Arg(0).String() == "get-master-addr-by-name" {
				//_, s := m.isLeader()
				addr, err := net.ResolveTCPAddr("tcp", m.tcpAddr)
				if err != nil {
					w.AppendError(err.Error())
					return
				}
				w.AppendArrayLen(2)
				w.AppendBulkString(addr.IP.String())
				w.AppendBulkString(strconv.Itoa(addr.Port))
			} else {
				w.AppendError(redeo.UnknownCommand(c.Name))
			}
		} else {
			w.AppendError("NOAUTH Authentication required.")
		}
	})
	m.redisSrv.Handle("subscribe", broker.Subscribe())
}

func (m *mars) getNodeNum() (int64, error) {
	getResponse, err := m.etcdCli.Get(context.Background(), workerKey, clientv3.WithPrefix())
	if err != nil {
		return 0, err
	}
	if getResponse.Count == 0 {
		return 0, nil
	}
	keySlice := make(map[string]string)
	for _, kv := range getResponse.Kvs {
		keySlice[strings.TrimLeft(string(kv.Key), workerKey)] = string(kv.Value)
	}
	m.log.Debug("worker num map:%v", keySlice)
	for i := 0; i <= 1024; i++ {
		_, ok := keySlice[strconv.Itoa(i)]
		if !ok {
			return int64(i), nil
		}
	}
	return 0, errors.New("can not get worker num")
}

func (m *mars) campaignLeader() (bool, string, error) {
	session, err := concurrency.NewSession(m.etcdCli)
	if err != nil {
		m.log.Error(err)
		return false, "", err
	}
	defer session.Close()
	mutex := concurrency.NewMutex(session, campaignKey)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	err = mutex.Lock(ctx)
	defer mutex.Unlock(ctx)
	if err != nil {
		m.log.Error(err)
		return false, "", err
	}
	kvc := clientv3.NewKV(m.etcdCli)
	grantResponse, err := m.etcdCli.Grant(context.Background(), 10)
	if err != nil {
		return false, "", err
	}
	response, err := kvc.Txn(context.Background()).
		If(clientv3util.KeyMissing(leaderKey)).
		Then(clientv3.OpPut(leaderKey, m.tcpAddr, clientv3.WithLease(grantResponse.ID))).
		Else(clientv3.OpGet(leaderKey)).
		Commit()
	if err != nil {
		m.log.Error(err)
		return false, "", err
	}

	if response.Succeeded {
		m.log.WithField("worker", m.tcpAddr).Info("campaign success")
		return true, m.tcpAddr, nil
	}

	for _, res := range response.Responses {
		t := (*clientv3.GetResponse)(res.GetResponseRange())
		for _, val := range t.Kvs {
			if string(val.Key) == leaderKey {
				return false, string(val.Value), nil
			}
		}
	}
	return false, "", TxnFailed
}

func (m *mars) isLeader() (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.leader, m.leaderAddr
}

func (m *mars) setLeader(b bool, s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leader = b
	m.leaderAddr = s
}

func (m *mars) keepLeader() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	grantResponse, err := m.etcdCli.Grant(context.Background(), m.etcdKeyTimeOut)
	if err != nil {
		m.log.WithField("worker", m.tcpAddr).Errorf("etcd grant err:%v", err)
		return
	}
	_, err = m.etcdCli.Put(ctx, leaderKey, m.tcpAddr, clientv3.WithLease(grantResponse.ID))
	if err != nil {
		m.log.WithField("worker", m.tcpAddr).Errorf("keep leader err:%v", err)
		return
	}
	m.log.WithField("worker", m.tcpAddr).Debugf("keep leader success")
}

func (m *mars) keepFollower() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	grantResponse, err := m.etcdCli.Grant(ctx, m.etcdKeyTimeOut)
	if err != nil {
		m.log.WithField("worker", m.tcpAddr).Errorf("etcd grant err:%v", err)
		return
	}
	_, err = m.etcdCli.Put(ctx, m.getEtcdNodeKey(), m.tcpAddr, clientv3.WithLease(grantResponse.ID))
	if err != nil {
		m.log.WithField("worker", m.tcpAddr).Errorf("keep follower err:%v", err)
		return
	}
	m.log.WithField("worker", m.tcpAddr).Debugf("keep follower success")
}

func (m *mars) keepWorkerNum() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	grantResponse, err := m.etcdCli.Grant(ctx, m.etcdKeyTimeOut)
	if err != nil {
		m.log.WithField("worker", m.tcpAddr).Errorf("etcd grant err:%v", err)
		return
	}
	_, err = m.etcdCli.Put(ctx, workerKey+"/"+strconv.FormatInt(m.workerNum, 10), m.tcpAddr, clientv3.WithLease(grantResponse.ID))
	if err != nil {
		m.log.Errorf("keep worker number error:%v", err)
		return
	}
	m.log.WithField("worker", m.tcpAddr).Debugf("keep worker number success number:%d", m.workerNum)
}

func (m *mars) getEtcdNodeKey() string {
	return nodeKey + "/" + m.tcpAddr
}

func (m *mars) watchLeader() {
	withField := m.log.WithField("worker", m.tcpAddr)
	withField.Trace("start watch")
	rch := m.etcdCli.Watch(context.Background(), leaderKey)
	for wresp := range rch {
		withField.Trace("get from watch")
		for _, ev := range wresp.Events {
			withField.Trace("loop events")
			withField.Debugf("leader update, type:%s,key:%q,value:%q", ev.Type, ev.Kv.Key, ev.Kv.Value)
			if ev.Type == mvccpb.DELETE {
				withField.Infof("leader node deleted, start campaign")
				m.campChan <- true
				return
			}
			_, leaderAddr := m.isLeader()
			if string(ev.Kv.Value) != leaderAddr {
				m.setLeader(m.tcpAddr == string(ev.Kv.Value), string(ev.Kv.Value))
			}
			continue
		}
	}
}

func (m *mars) initialize() error {
	b, s, err := m.campaignLeader()
	if err != nil {
		m.log.Errorf("campaign leader err: %v", err)
		return err
	}
	if b || (!b && s == m.tcpAddr) {
		num, err := m.getNodeNum()
		if err != nil {
			err = errors.Wrap(err, "leader get node num err")
			return err
		}
		m.workerNum = num
		m.log.Infof("generator id:%d", num)
		m.gen = generator.New(num)
		m.keepWorkerNum()
		m.setLeader(b, s)
		return nil
	}
	if !b && s != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		grantResponse, err := m.etcdCli.Grant(context.Background(), m.etcdKeyTimeOut)
		if err != nil {
			m.log.WithField("worker", m.tcpAddr).Errorf("etcd grant err:%v", err)
			return err
		}
		_, err = m.etcdCli.Put(ctx, m.getEtcdNodeKey(), m.tcpAddr, clientv3.WithLease(grantResponse.ID))
		if err != nil {
			err = errors.Wrap(err, "initialize to put node err")
			return err
		}
		client := redis.NewClient(&redis.Options{
			Addr:     s,
			Password: m.redisPasswd,
			DB:       0, // use default DB
		})
		s, err := client.Get("node").Result()
		client.Close()
		if err != nil {
			err = errors.Wrap(err, "initialize get node number err")
			return err
		}
		m.log.Infof("get node num from leader:%s", s)
		parseInt, err := strconv.ParseInt(s, 10, 64)
		m.log.Infof("generator id:%d", parseInt)
		m.workerNum = parseInt
		m.gen = generator.New(parseInt)
		go m.watchLeader()
		return nil
	}

	return nil
}

func (m *mars) chanFuc() {
	for {
		select {
		case x := <-m.keepChan:
			if x {
				m.keepWorkerNum()
				b, _ := m.isLeader()
				if b {
					m.keepLeader()
				} else {
					m.keepFollower()
				}
			}
		case <-m.ticker.C:
			m.keepChan <- true
		case x := <-m.campChan:
			if x {
				b, s, err := m.campaignLeader()
				m.log.Debugf("campaign result:%t,leader:%s, this.tcp:%s", b, s, m.tcpAddr)
				if err != nil {
					m.log.Error(err)
					continue
				}
				m.setLeader(b, s)
			}
		}
	}
}

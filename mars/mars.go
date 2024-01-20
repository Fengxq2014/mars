package mars

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Fengxq2014/mars/etcdsrv"
	"github.com/Fengxq2014/mars/generator"
	"github.com/Fengxq2014/mars/sequence"
	"github.com/bsm/redeo"
	"github.com/bsm/redeo/resp"
	"github.com/go-redis/redis"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
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
	campChan       chan bool
	ticker         *time.Ticker
	etcdKeyTimeOut int64
	workerNum      int64
	redisPasswd    string
	httpName       string
	httpPasswd     string
	seqMap         map[string]*sequence.Sequence
}

type MarsConfig struct {
	HttpAddr      string
	HttpTimeOut   time.Duration
	TcpAddr       string
	Log           *log.Logger
	EtcdEndPoints string
	RedisPasswd   string
	HttpName      string
	HttpPasswd    string
	AppKey        string
	Ip            string
}

var (
	campaignKey = "mars/campaign"
	leaderKey   = "mars/node/leader"
	nodeKey     = "mars/node"
	workerKey   = "mars/worker"
	version     = "1.3.0"
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
				EtcdEndPoints: "localhost:2379",
				HttpTimeOut:   10 * time.Second,
			}
		}
		campaignKey = cfg.AppKey + "/" + campaignKey
		leaderKey = cfg.AppKey + "/" + leaderKey
		nodeKey = cfg.AppKey + "/" + nodeKey
		workerKey = cfg.AppKey + "/" + workerKey
		r := mux.NewRouter()
		if cfg.HttpName != "" && cfg.HttpPasswd != "" {
			amw := authenticationMiddleware{}
			amw.Populate(cfg.HttpName, cfg.HttpPasswd)
			r.Use(amw.Middleware)
		}

		r.HandleFunc("/id", GetID).Methods("GET")
		r.HandleFunc("/id53", GetID53).Methods("GET")
		r.HandleFunc("/info/{id:[0-9]+}", GetIDInfo).Methods("GET")
		r.HandleFunc("/seq/{id}", GetSeq).Methods("GET")

		if cfg.Ip != "" {
			cfg.HttpAddr = cfg.Ip + ":" + strings.Split(cfg.HttpAddr, ":")[1]
			cfg.TcpAddr = cfg.Ip + ":" + strings.Split(cfg.TcpAddr, ":")[1]
		}

		s := &http.Server{
			Addr:           cfg.HttpAddr,
			Handler:        r,
			ReadTimeout:    cfg.HttpTimeOut,
			WriteTimeout:   cfg.HttpTimeOut,
			MaxHeaderBytes: 1 << 20,
		}
		m = &mars{
			httpAddr:       cfg.HttpAddr,
			tcpAddr:        cfg.TcpAddr,
			httpSrv:        s,
			redisSrv:       redeo.NewServer(nil),
			etcdCli:        etcdsrv.New(cfg.EtcdEndPoints).Cli,
			log:            cfg.Log,
			campChan:       make(chan bool, 1),
			ticker:         time.NewTicker(time.Second * 7),
			etcdKeyTimeOut: 10,
			redisPasswd:    cfg.RedisPasswd,
			httpName:       cfg.HttpName,
			httpPasswd:     cfg.HttpPasswd,
			seqMap:         make(map[string]*sequence.Sequence),
		}
		var seqConf []sequence.Config
		getenv := os.Getenv("SEQ.CONF")
		if getenv != "" {
			seqSetting([]byte(getenv), seqConf, cfg)
		} else {
			_, err := os.Stat("./seq.conf")
			if err == nil {

				file, err := os.ReadFile("./seq.conf")
				if err != nil {
					m.log.Fatalf("Error reading seq.conf file:", err)
				}
				seqSetting(file, seqConf, cfg)
			} else if os.IsNotExist(err) {
				m.log.Warnf("文件 %s 不存在\n", "seq.conf")
			} else {
				m.log.Warn("发生了其他错误", err.Error())
			}
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

func seqSetting(file []byte, seqConf []sequence.Config, cfg *MarsConfig) {
	if err := json.Unmarshal(file, &seqConf); err != nil {
		m.log.Fatalf("Error parse seq.conf file:", err)
	}
	for _, config := range seqConf {
		m.seqMap[config.Id] = sequence.New(m.etcdCli, cfg.AppKey, m.log, config.Id, config.TimeRollback, config.NumRollback)
	}
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
	m.redisSrv.HandleFunc("hgetall", func(w resp.ResponseWriter, c *resp.Command) {
		cli := redeo.GetClient(c.Context())
		value := cli.Context().Value(ctxAuthOK{})
		b, ok := value.(bool)
		if ok && b {
			if c.ArgN() != 1 {
				w.AppendError(redeo.WrongNumberOfArgs(c.Name))
				return
			} else {
				id := c.Arg(0).String()
				info, err := m.getIdInfo(id)
				if err != nil {
					w.AppendError(err.Error())
					return
				}
				w.AppendArrayLen(6)
				w.AppendBulkString("time")
				w.AppendBulkString(info.Time)
				w.AppendBulkString("step")
				w.AppendBulkString(info.Step)
				w.AppendBulkString("node")
				w.AppendBulkString(info.Node)
			}
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
			if c.Arg(0).String() == "id53" {
				return m.gen.GetStr53()
			}
			if strings.HasPrefix(c.Arg(0).String(), "seq/") {
				id, _ := strings.CutPrefix(c.Arg(0).String(), "seq/")
				if s, ok := m.seqMap[id]; ok {
					next, err := s.Next()
					if err != nil {
						return redeo.ErrUnknownCommand(err.Error())
					}
					return next
				}
				return redeo.ErrUnknownCommand(c.Arg(0).String())
			}
			return nil
		} else {
			return errors.New("NOAUTH Authentication required.")
		}
	}))
	m.redisSrv.HandleFunc("lrange", func(w resp.ResponseWriter, c *resp.Command) {
		cli := redeo.GetClient(c.Context())
		value := cli.Context().Value(ctxAuthOK{})
		b, ok := value.(bool)
		if ok && b {
			if c.ArgN() != 3 {
				w.AppendError(redeo.WrongNumberOfArgs(c.Name))
				return
			}
			start, err := c.Arg(1).Int()
			if err != nil {
				w.AppendError(redeo.WrongNumberOfArgs(c.Arg(1).String()))
				return
			}
			end, err := c.Arg(2).Int()
			if err != nil {
				w.AppendError(redeo.WrongNumberOfArgs(c.Arg(1).String()))
				return
			}
			if start >= end {
				w.AppendError(redeo.UnknownCommand("start >= end"))
				return
			}
			num := int(end - start)
			if num > maxNum {
				w.AppendError(redeo.WrongNumberOfArgs(fmt.Sprintf("max num is %d", maxNum)))
				return
			}
			if c.Arg(0).String() == "id" {
				w.AppendArrayLen(num)
				for i := 0; i < num; i++ {
					w.AppendBulkString(m.gen.GetStr())
				}
				return
			}
			if c.Arg(0).String() == "id53" {
				w.AppendArrayLen(num)
				for i := 0; i < num; i++ {
					w.AppendBulkString(m.gen.GetStr53())
				}
				return
			}
			if strings.HasPrefix(c.Arg(0).String(), "seq/") {
				id, _ := strings.CutPrefix(c.Arg(0).String(), "seq/")
				if s, ok := m.seqMap[id]; ok {
					for i := 0; i < num; i++ {
						next, err := s.Next()
						if err != nil {
							w.AppendError(redeo.UnknownCommand(c.Arg(0).String()))
							return
						}
						w.AppendBulkString(next)
					}
					return
				}
				w.AppendError(redeo.UnknownCommand(c.Arg(0).String()))
				return
			}
		} else {
			w.AppendError("NOAUTH Authentication required.")
			return
		}
	})
	m.redisSrv.HandleFunc("sentinel", func(w resp.ResponseWriter, c *resp.Command) {
		//cli := redeo.GetClient(c.Context())
		//value := cli.Context().Value(ctxAuthOK{})
		//b, ok := value.(bool)
		//if ok && b {
		if strings.ToLower(c.Arg(0).String()) == "get-master-addr-by-name" {
			_, s := m.isLeader()
			addr, err := net.ResolveTCPAddr("tcp", s)
			if err != nil {
				m.log.Errorf("sentinel, leaderAddr:%s, err:%v", s, err)
				w.AppendError(err.Error())
				return
			}
			w.AppendArrayLen(2)
			w.AppendBulkString(addr.IP.String())
			w.AppendBulkString(strconv.Itoa(addr.Port))
		} else {
			m.log.Errorf("unknown command:%v %v", c.Name, c.Args)
			w.AppendError(redeo.UnknownCommand(c.Name))
		}
		//} else {
		//	w.AppendError("NOAUTH Authentication required.")
		//}
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
	s, err := concurrency.NewSession(m.etcdCli, concurrency.WithTTL(*(*int)(unsafe.Pointer(&m.etcdKeyTimeOut))))
	if err != nil {
		return false, "", err
	}
	election := concurrency.NewElection(s, campaignKey)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	leader, err := election.Leader(ctx)
	if err != nil {
		err := election.Campaign(ctx, m.tcpAddr)
		if err != nil {
			return false, "", err
		}
		return true, m.tcpAddr, nil
	}
	return false, string(leader.Kvs[0].Value), nil
}

func (m *mars) isLeader() (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.leader, m.leaderAddr
}

func (m *mars) setLeader(b bool, s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.log.Infof("set leader, %v, %s", b, s)
	m.leader = b
	m.leaderAddr = s
}

func (m *mars) keepKey(key string) error {
	withField := m.log.WithField("worker", m.tcpAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	grantResponse, err := m.etcdCli.Grant(ctx, m.etcdKeyTimeOut)
	if err != nil {
		withField.Errorf("etcd grant err:%v", err)
		return err
	}
	_, err = m.etcdCli.Put(ctx, key, m.tcpAddr, clientv3.WithLease(grantResponse.ID))
	if err != nil {
		m.log.Errorf("keep worker number error:%v", err)
		return err
	}
	aliveResponses, err := m.etcdCli.KeepAlive(context.TODO(), grantResponse.ID)
	if err != nil {
		withField.Errorf("keep worker number error: %v", err)
		return err
	}
	withField.Debugf("keep worker number success number:%d", m.workerNum)
	go func() {
		for {
			select {
			case r, ok := <-aliveResponses:
				if ok {
					withField.Debugf("keep alive TTL:%v", r.TTL)
				} else {
					withField.Error("keep alive chan closed")
					return
				}
			}
		}
	}()
	return nil
}

func (m *mars) getEtcdNodeKey() string {
	return nodeKey + "/" + m.tcpAddr
}

func (m *mars) watchLeader() {
	withField := m.log.WithField("worker", m.tcpAddr)
	withField.Trace("start watch")
	rch := m.etcdCli.Watch(context.Background(), campaignKey, clientv3.WithPrefix())
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
	m.log.Infof("campaign result: %v, addr:%v", b, s)
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
		err = m.keepKey(workerKey + "/" + strconv.FormatInt(m.workerNum, 10))
		if err != nil {
			return err
		}
		m.setLeader(b, s)
		return nil
	}
	if !b && s != "" {
		err := m.keepKey(m.getEtcdNodeKey())
		if err != nil {
			return err
		}
		client := redis.NewClient(&redis.Options{
			Addr:     s,
			Password: m.redisPasswd,
			DB:       0, // use default DB
		})
		num, err := client.Get("node").Result()
		client.Close()
		if err != nil {
			err = errors.Wrap(err, "initialize get node number err")
			return err
		}
		m.log.Infof("get node num from leader:%s", num)
		parseInt, err := strconv.ParseInt(num, 10, 64)
		m.log.Infof("generator id:%d", parseInt)
		m.workerNum = parseInt
		err = m.keepKey(workerKey + "/" + strconv.FormatInt(m.workerNum, 10))
		if err != nil {
			return err
		}
		if err != nil {
			return err
		}
		m.gen = generator.New(parseInt)
		go m.watchLeader()
		m.setLeader(b, s)
		return nil
	}

	return nil
}

func (m *mars) chanFuc() {
	for {
		select {
		case x := <-m.campChan:
			if x {
				b, s, err := m.campaignLeader()
				m.log.Debugf("campaign result:%t,leader:%s, this.tcp:%s", b, s, m.tcpAddr)
				if err != nil {
					m.log.Error(err)
					m.log.Infof("master:%s", m.leaderAddr)
					continue
				}
				m.setLeader(b, s)
			}
		}
	}
}

type IdInfo struct {
	Time string
	Step string
	Node string
}

func (m *mars) getIdInfo(id string) (IdInfo, error) {
	res := IdInfo{}
	i, err := strconv.Atoi(id)
	if err != nil {
		return res, errors.New(fmt.Sprintf("%s 不是合法的id", id))
	}
	response, err := m.etcdCli.Get(context.Background(), workerKey+"/"+strconv.FormatInt(m.gen.GetNode(int64(i)), 10))
	if err != nil {
		return res, err
	}
	var node string
	if response.Count < 1 {
		node = ""
	} else {
		node = string(response.Kvs[0].Value)
	}
	res.Time = m.gen.GetTime(int64(i))
	res.Step = strconv.FormatInt(m.gen.GetStep(int64(i)), 10)
	res.Node = node
	return res, nil
}

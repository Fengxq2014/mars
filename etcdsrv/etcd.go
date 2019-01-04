package etcdsrv

import (
	"github.com/coreos/etcd/clientv3"
	"strings"
	"sync"
	"time"
)

type etcdSrv struct {
	Cli *clientv3.Client
}

var once sync.Once
var temp *etcdSrv

func New(endpoints string) *etcdSrv {
	once.Do(func() {
		c, err := clientv3.New(clientv3.Config{
			Endpoints:   strings.Split(endpoints, ","),
			DialTimeout: 5 * time.Second,
		})
		if err != nil {
			panic(err)
		}
		temp = &etcdSrv{
			Cli: c,
		}
	})
	return temp
}

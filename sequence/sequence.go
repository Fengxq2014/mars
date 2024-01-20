package sequence

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	clientv3 "go.etcd.io/etcd/client/v3"
	"strconv"
	"time"
)

type Sequence struct {
	Id           string
	TimeRollback string
	NumRollback  int64
	etcdCli      *clientv3.Client
	appKey       string
	log          *log.Logger
}

type Config struct {
	Id           string `json:"id"`
	TimeRollback string `json:"timeRollback"`
	NumRollback  int64  `json:"numRollback"`
}

func New(etcdCli *clientv3.Client, appKey string, log *log.Logger, id string, t string, max int64) *Sequence {
	return &Sequence{
		Id:           id,
		TimeRollback: t,
		NumRollback:  max,
		etcdCli:      etcdCli,
		appKey:       appKey,
		log:          log,
	}
}

func (s *Sequence) Next() (string, error) {
	var put clientv3.Op
	key, sec := s.getKeyAndSec()
	if sec > 0 {
		var lease *clientv3.LeaseGrantResponse
		var err error
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		lease, err = s.etcdCli.Grant(ctx, int64(sec))
		if err != nil {
			s.log.Errorf("lease err %s", err.Error())
			return "", err
		}
		put = clientv3.OpPut(key, "1", clientv3.WithLease(lease.ID))
	} else {
		put = clientv3.OpPut(key, "1")
	}

	s.log.Infof("key %s, sec %d", key, sec)
	tx, err := s.etcdCli.Txn(context.TODO()).Then(put, clientv3.OpGet(key)).Commit()
	if err != nil {
		s.log.Errorf("事务err,%s", err.Error())
		return "", err
	}
	if tx.Succeeded {
		if len(tx.Responses) == 2 {
			s.log.Info("事务成功返回结果,", tx.Responses[1].GetResponseRange().Kvs[0].Version)
			s.numRoll(tx.Responses[1].GetResponseRange().Kvs[0].Version, key)
			return strconv.FormatInt(tx.Responses[1].GetResponseRange().Kvs[0].Version, 10), nil
		}
		return "", errors.Errorf("response count err:%d, key:%s", len(tx.Responses), key)
	}
	return "", errors.New("tx err")
}

func (s *Sequence) numRoll(version int64, key string) {
	if s.NumRollback == 0 {
		return
	}
	if s.NumRollback <= version {
		_, err := s.etcdCli.Delete(context.TODO(), key)
		if err != nil {
			s.log.Errorf("numRoll del err %s", err.Error())
		}
	}
}

func (s *Sequence) getKeyAndSec() (string, int) {
	now := time.Now()
	if s.TimeRollback == "" {
		return s.appKey + "/seq/", 0
	}
	tmp := s.appKey + "/seq/"
	if s.TimeRollback == "m" {
		tmp += fmt.Sprintf("%d%d%d%d%d", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute())
		return tmp, 60 - now.Second()
	}
	if s.TimeRollback == "h" {
		tmp += fmt.Sprintf("%d%d%d%d", now.Year(), now.Month(), now.Day(), now.Hour())
		return tmp, (60-now.Minute())*60 + (60 - now.Second())
	}
	if s.TimeRollback == "d" {
		tmp += fmt.Sprintf("%d%d%d", now.Year(), now.Month(), now.Day())
		return tmp, (24-now.Hour())*3600 + (60-now.Minute())*60 + (60 - now.Second())
	}
	if s.TimeRollback == "M" {
		tmp += fmt.Sprintf("%d%d", now.Year(), now.Month())
		daysInMonth := now.AddDate(0, 1, -now.Day()).Day()
		return tmp, ((daysInMonth - now.Day()) * 24 * 3600) + (24-now.Hour())*3600 + (60-now.Minute())*60 + (60 - now.Second())
	}
	if s.TimeRollback == "y" {
		tmp += fmt.Sprintf("%d", now.Year())
		daysInMonth := now.AddDate(0, 1, -now.Day()).Day()
		lastDayOfYear := time.Date(now.Year(), time.December, 31, 0, 0, 0, 0, time.UTC).YearDay()
		return tmp, ((lastDayOfYear - now.YearDay()) * 24 * 3600) + ((daysInMonth - now.Day()) * 24 * 3600) + (24-now.Hour())*3600 + (60-now.Minute())*60 + (60 - now.Second())
	}
	return tmp, 0
}

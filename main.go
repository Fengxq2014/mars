package main

import (
	"flag"
	"github.com/Fengxq2014/mars/mars"
	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"os"
	"time"
)

var (
	dialTimeout = 5 * time.Second
	httpAddr    string
	tcpAddr     string
	etcdAddrs   string
	verbose     bool
	redisPasswd string
	httpName    string
	httpPasswd  string
)

func getEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("can't load value of %s", key)
	}
	return value
}

func init() {
	flag.StringVar(&httpAddr, "http", "", "http address to listen on")
	flag.StringVar(&tcpAddr, "tcp", "", "tcp address to listen on")
	flag.StringVar(&etcdAddrs, "etcd", "", "etcd addrs")
	flag.BoolVar(&verbose, "v", false, "verbose log")
	flag.StringVar(&redisPasswd, "redisPasswd", "", "redis auth pass word")
	flag.StringVar(&httpName, "httpName", "", "http basic auth name")
	flag.StringVar(&httpPasswd, "httpPasswd", "", "http basic auth pass word")
}

func main() {
	flag.Parse()
	getValueFromEnv(&httpAddr, "MARS_HTTP_ADDR")
	getValueFromEnv(&tcpAddr, "MARS_TCP_ADDR")
	getValueFromEnv(&etcdAddrs, "MARS_ETCD_ENDPOINTS")
	getValueFromEnv(&redisPasswd, "MARS_REDIS_PASSWD")
	getValueFromEnv(&httpName, "MARS_HTTP_NAME")
	getValueFromEnv(&httpPasswd, "MARS_HTTP_PASSWD")
	logger := log.New()
	if verbose {
		logger.SetLevel(log.DebugLevel)
	} else {
		logger.SetLevel(log.InfoLevel)
	}
	logger.SetOutput(os.Stdout)
	logger.SetReportCaller(true)
	logger.SetFormatter(&log.TextFormatter{
		QuoteEmptyFields: true,
		FullTimestamp:    true,
		ForceColors:      true,
	})

	cfg := &mars.MarsConfig{
		HttpAddr:      httpAddr,
		TcpAddr:       tcpAddr,
		Log:           logger,
		EtcdEndPoints: etcdAddrs,
		RedisPasswd:   redisPasswd,
		HttpName:      httpName,
		HttpPasswd:    httpPasswd,
		HttpTimeOut:   dialTimeout,
	}

	m := mars.New(cfg)
	m.StartTcp()
	m.StartHttp()
}

func getValueFromEnv(key *string, envKey string) {
	if *key == "" {
		err := godotenv.Load()
		if err != nil {
			panic(envKey + " is empty and .env file not found")
		}
		*key = getEnv(envKey)
	}
}

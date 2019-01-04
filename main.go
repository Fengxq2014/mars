package main

import (
	"flag"
	"github.com/Fengxq2014/mars/mars"
	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"net"
	"os"
	"time"
)

var (
	dialTimeout = 5 * time.Second
	httpAddr    string
	tcpAddr     string

	TcpAddr *net.TCPAddr
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
}

func main() {
	flag.Parse()
	if httpAddr == "" {
		err := godotenv.Load()
		if err != nil {
			panic("http address in empty and .env file not found")
		}
		httpAddr = getEnv("MARS_HTTP_ADDR")
	}
	if tcpAddr == "" {
		err := godotenv.Load()
		if err != nil {
			panic("tcp address in empty and .env file not found")
		}
		httpAddr = getEnv("MARS_TCP_ADDR")
	}
	logger := log.New()
	logger.SetLevel(log.InfoLevel)
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
		EtcdEndPoints: "localhost:23790",
		RedisPasswd:   "123",
		HttpName:      "user",
		HttpPasswd:    "pwd",
	}

	m := mars.New(cfg)
	m.StartTcp()
	m.StartHttp()
}

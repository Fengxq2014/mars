```base
                           
  _ __ ___   __ _ _ __ ___ 
 | '_ ` _ \ / _` | '__/ __|
 | | | | | | (_| | |  \__ \
 |_| |_| |_|\__,_|_|  |___/
         
```
# Mars(阿瑞斯)

[![Go Report Card](https://goreportcard.com/badge/github.com/Fengxq2014/mars)](https://goreportcard.com/report/github.com/Fengxq2014/mars)
[![GoDoc](https://godoc.org/github.com/Fengxq2014/mars?status.svg)](https://godoc.org/github.com/Fengxq2014/mars)
[![Build Status](https://travis-ci.org/Fengxq2014/mars.svg?branch=master)](https://travis-ci.org/Fengxq2014/mars)

一个分布式、高性能的雪花id生成器

## 依赖
* etcd 一个高可用的分布式键值(key-value)数据库，etcd内部采用raft协议作为一致性算法

## 使用
* http
   * 获取id
   ```base
   $ curl -u name:passwd 'http://{host}/id'
   $ 530749286099451904%
   ```
   * 解析id信息
   ```$xslt
   $ curl -u name:passwd 'http://{host}/info/530703474355077120'
   $ {"node":"192.168.1.65:9736","step":"0","time":"2019-01-04T11:06:08.28+08:00"}%
   ```
* tcp
   * 使用redis-cli
   ```base
   $ redis-cli auth
   $ get id
   ```
   * 使用其他redis client 请使用redis sentinel 模式 
   
## 用户认证
### http
使用basic auth
### tcp
使用redis auth

## 节点状态流程图
[![image.png](https://i.postimg.cc/jdtXdGKv/image.png)](https://postimg.cc/z32Wx2yR)

## 性能
### http

```bash
Running 30s test @ http://localhost:8081/id
  16 threads and 400 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency     4.99ms  782.98us  10.69ms   78.32%
    Req/Sec     4.91k   632.05     8.32k    85.78%
  Latency Distribution
     50%    5.15ms
     75%    5.45ms
     90%    5.77ms
     99%    6.43ms
  2348646 requests in 30.10s, 374.05MB read
  Socket errors: connect 0, read 237, write 0, timeout 0
  Non-2xx or 3xx responses: 2348646
Requests/sec:  78021.13
Transfer/sec:     12.43MB
```
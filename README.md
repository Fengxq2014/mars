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
* 标准雪花id
* 53位兼容前端js精度的短雪花id
* 自增序列（按时间【分钟、小时、天、月、年】回退、按最大值回退）
* 批量获取id

## 依赖
* etcd 一个高可用的分布式键值(key-value)数据库，etcd内部采用raft协议作为一致性算法

## Worker 租约与升级

* 同一 ID 空间的所有实例必须使用同一个 etcd 集群和 AppKey，并保持主机时钟同步。
* 实例直接通过 etcd 事务抢占 worker key（不存在才写入），不再向 leader 查询空闲编号后覆盖写入。
  两种 ID 格式共用编号，因此每个 AppKey 最多运行 32 个发号实例（worker 0–31）。
* 续租失败、worker key 归属改变或本地租约期限到期后，该实例停止发号，直到进程重启。
  HTTP `/id`、`/id53` 返回 503；Redis GET/LRANGE 返回错误，批量响应不会输出部分 ID。
  部署时应通过服务监督程序或存活检查重启这些实例。网络恢复不会自动重新启用旧的生成器。
* worker 编号重用后至少等待一秒再发号，避免 53 位 ID 在同一秒内从序号零重新开始。
  正常停止时不提前释放已使用的 worker 租约，由 etcd 等待其 TTL 到期。
* **升级时需要先停止同一 AppKey 下的全部旧版本实例，再启动新版本，不能混用滚动升级。**
  旧版的无条件写入仍会覆盖 worker key，无法由新版的事务单方面保证互斥。
  本次修改保持 ID 格式和两个 epoch 不变。

## 测试

```sh
go test ./...
go test -race ./...
# 可选：使用本地 etcd 可执行文件运行真实事务和租约集成测试。
# 测试自动创建临时目录，并只监听 127.0.0.1，不访问现有 etcd 集群。
MARS_TEST_ETCD_BINARY=/path/to/etcd go test ./mars -run TestRealEtcd -v
```

## 使用
* http
   * 获取id
   ```base
   $ curl -u name:passwd 'http://{host}/id'
   $ 530749286099451904%
   $ # 批量获取
   $ curl -u name:passwd 'http://{host}/id?num=10'
   ```
  * 获取53位id
   ```base
   $ curl -u name:passwd 'http://{host}/id53'
   $ 4122703036416%
   $ # 批量获取
   $ curl -u name:passwd 'http://{host}/id53?num=10'
   ```
  * 获取序列号
   ```base
   $ # {id}：为序列号id，需要按照业务规则进行预先配置
   $ curl -u name:passwd 'http://{host}/seq/{id}'
   $ 1%
   $ # 批量获取
   $ curl -u name:passwd 'http://{host}/seq/{id}?num=10'
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
   $ # 获取一个标准id
   $ get id
   $ #批量获取10个
   $ lrange id 0 10
  
  
   $ # 获取一个53位id
   $ get id53
   $ #批量获取10个
   $ lrange id53 0 10
  
  
   $ # 获取一个序列号 {id}：为序列号id，需要按照业务规则进行预先配置
   $ get seq/{id}
   $ #批量获取10个
   $ lrange seq/{id} 0 10
   ```
   * 使用其他redis client 请使用redis sentinel 模式 
   
## 用户认证
### http
使用basic auth
### tcp
使用redis auth

## seq.conf配置
```json
[
  {
    "id": "1",
    "timeRollback": "m", // 按时间回退（m：分钟，h：小时，d：天，M：月，y：年）
    "numRollback": 1000 // 按最大值回退
  }
]
```

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

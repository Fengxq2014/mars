package snowflake53

import (
	"errors"
	"strconv"
	"sync"
	"time"
)

const (
	workerBits  uint8 = 5                       // 节点数
	seqBits     uint8 = 16                      // 1秒内可生成的id序号的二进制位数
	workerMax   int64 = -1 ^ (-1 << workerBits) // 节点ID的最大值，用于防止溢出
	seqMax      int64 = -1 ^ (-1 << seqBits)    // 同上，用来表示生成id序号的最大值
	timeShift   uint8 = workerBits + seqBits    // 时间戳向左的偏移量
	workerShift uint8 = seqBits                 // 节点ID向左的偏移量
	epoch       int64 = 1703751314              // 开始运行时间
)

type Node struct {
	// 添加互斥锁 确保并发安全
	mu sync.Mutex
	// 记录时间戳
	timestamp int64
	// 该节点的ID
	nodeId int64
	// 当前秒已经生成的id序列号(从0开始累加) 1秒内最多生成65535个ID
	seq int64
}

// 实例化对象
func NewNode(nodeId int64) (*Node, error) {
	// 要先检测workerId是否在上面定义的范围内
	if nodeId < 0 || nodeId > workerMax {
		return nil, errors.New("Node ID excess of quantity")
	}
	// 生成一个新节点
	return &Node{
		timestamp: 0,
		nodeId:    nodeId,
		seq:       0,
	}, nil
}

// 获取一个新ID
func (n *Node) Int64() int64 {
	// 获取id最关键的一点 加锁 加锁 加锁
	n.mu.Lock()
	defer n.mu.Unlock() // 生成完成后记得 解锁 解锁 解锁
	// 获取生成时的时间戳
	now := time.Now().UnixNano() / 1e9 // 纳秒转秒
	if n.timestamp == now {
		n.seq = (n.seq + 1) & seqMax
		// 这里要判断，当前工作节点是否在1秒内已经生成seqMax个ID
		if n.seq == 0 {
			// 如果当前工作节点在1秒内生成的ID已经超过上限 需要等待1秒再继续生成
			for now <= n.timestamp {
				now = time.Now().UnixNano() / 1e9
			}
		}
	} else {
		// 如果当前时间与工作节点上一次生成ID的时间不一致 则需要重置工作节点生成ID的序号
		n.seq = 0
	}
	n.timestamp = now // 将机器上一次生成ID的时间更新为当前时间
	// 第一段 now - epoch 为该算法目前已经奔跑了xxx秒
	// 如果在程序跑了一段时间修改了epoch这个值 可能会导致生成相同的ID
	ID := int64((now-epoch)<<timeShift | (n.nodeId << workerShift) | (n.seq))
	return ID
}

func (n *Node) String() string {
	return strconv.FormatInt(n.Int64(), 10)
}

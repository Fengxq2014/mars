package generator

import (
	"github.com/bwmarrin/snowflake"
	"sync"
	"time"
)

type Generator struct {
	workerID int64
	node     *snowflake.Node
}

var genTmp *Generator
var once sync.Once

func New(id int64) *Generator {
	once.Do(func() {
		snowflake.Epoch = 1420041600000
		n, err := snowflake.NewNode(id)
		if err != nil {
			panic(err)
		}
		genTmp = &Generator{
			node:     n,
			workerID: id,
		}
	})
	return genTmp
}

func (gen Generator) GetID() int64 {
	return (int64)(gen.node.Generate())
}

func (gen Generator) GetStr() string {
	return gen.node.Generate().String()
}

func (gen Generator) GetTime(i int64) string {
	t := snowflake.ID(i).Time()
	ti := time.Unix(t/1000, (t%1000)*int64(time.Millisecond/time.Nanosecond)).Local()
	return ti.Format(time.RFC3339Nano)
}

func (gen Generator) GetStep(i int64) int64 {
	return snowflake.ID(i).Step()
}

func (gen Generator) GetNode(i int64) int64 {
	return snowflake.ID(i).Node()
}

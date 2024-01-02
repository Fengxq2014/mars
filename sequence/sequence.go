package sequence

//
//import (
//	"context"
//	"fmt"
//	clientv3 "go.etcd.io/etcd/client/v3"
//	"strconv"
//)
//
//type Sequence struct {
//	Id           string
//	Format       string
//	TimeRollback string
//	NumRollback  string
//	etcdCli      *clientv3.Client
//}
//
//func (s *Sequence) New(etcdCli *clientv3.Client) *Sequence {
//	return &Sequence{
//		Id:           "",
//		Format:       "",
//		TimeRollback: "",
//		NumRollback:  "",
//		etcdCli:      etcdCli,
//	}
//}
//
//func (s *Sequence) Next() string {
//	resp, err := s.etcdCli.Txn(context.TODO()).
//		If(clientv3.Compare(clientv3.Value("counter"), "=", "")).
//		Then(clientv3.OpPut("counter", "1")).
//		Else(clientv3.OpGet("counter")).
//		Commit()
//
//	if err != nil {
//		fmt.Println("Error:", err)
//		return
//	}
//
//	if !resp.Succeeded {
//		// Key already exists, increment the value
//		currentValue := resp.Responses[0].GetResponseRange().Kvs[0].Value
//		fmt.Println("Current value:", string(currentValue))
//		atoi, _ := strconv.Atoi(string(currentValue))
//		newValue := atoi + 1
//
//		// Increment the value
//		_, err := s.etcdCli.Put(context.TODO(), "counter", strconv.Itoa(newValue))
//		if err != nil {
//			fmt.Println("Error incrementing value:", err)
//			return
//		}
//		fmt.Println("Incremented value:", newValue)
//	}
//	return ""
//}

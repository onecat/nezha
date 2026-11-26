package rpc

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/naiba/nezha/proto"
)

type concurrentTaskTestStream struct {
	pb.NezhaService_RequestTaskServer
	ctx context.Context
	active atomic.Int32
	overlap atomic.Bool
	count atomic.Int32
}
func(s *concurrentTaskTestStream) Context() context.Context{return s.ctx}
func(s *concurrentTaskTestStream) Send(*pb.Task)error{
	if s.active.Add(1)!=1{s.overlap.Store(true)}
	defer s.active.Add(-1)
	time.Sleep(time.Millisecond)
	s.count.Add(1)
	return nil
}

func TestSerialTaskStream(t *testing.T){
	ctx,cancel:=context.WithCancel(context.Background())
	defer cancel()
	raw:=&concurrentTaskTestStream{ctx:ctx}
	s:=newSerialTaskStream(raw)
	var wg sync.WaitGroup
	for i:=0;i<64;i++{wg.Add(1);go func(){defer wg.Done();if err:=s.Send(&pb.Task{});err!=nil{t.Error(err)}}()}
	wg.Wait()
	if raw.overlap.Load()||raw.count.Load()!=64{t.Fatal("concurrent send or lost task")}
	cancel()
	if err:=s.Send(&pb.Task{});err==nil{t.Fatal("send accepted after cancellation")}
}

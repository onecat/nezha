package rpc

import (
	"context"
	"time"

	pb "github.com/naiba/nezha/proto"
)

// gRPC permits one sending goroutine per stream. All task producers share
// this wrapper. Waiters have a bounded acquisition time; cancellation
// releases waiting senders even when a peer has disconnected.
type serialTaskStream struct {
	pb.NezhaService_RequestTaskServer
	slot chan struct{}
}

func newSerialTaskStream(stream pb.NezhaService_RequestTaskServer) *serialTaskStream {
	return &serialTaskStream{NezhaService_RequestTaskServer:stream,slot:make(chan struct{},1)}
}

func (s *serialTaskStream) Send(task *pb.Task) error {
	ctx:=s.Context()
	timer:=time.NewTimer(5*time.Second)
	defer timer.Stop()
	select {
	case s.slot<-struct{}{}:
		defer func(){<-s.slot}()
	case <-ctx.Done(): return ctx.Err()
	case <-timer.C: return context.DeadlineExceeded
	}
	if err:=ctx.Err();err!=nil{return err}
	return s.NezhaService_RequestTaskServer.Send(task)
}

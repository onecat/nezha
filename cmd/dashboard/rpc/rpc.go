package rpc

import (
	"fmt"
	"net"

	"google.golang.org/grpc"

	"github.com/naiba/nezha/model"
	pb "github.com/naiba/nezha/proto"
	rpcService "github.com/naiba/nezha/service/rpc"
	"github.com/naiba/nezha/service/singleton"
)

func ServeRPC(port uint) {
	server := grpc.NewServer()
	rpcService.NezhaHandlerSingleton = rpcService.NewNezhaHandler()
	pb.RegisterNezhaServiceServer(server, rpcService.NezhaHandlerSingleton)
	listen, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		panic(err)
	}
	server.Serve(listen)
}

func DispatchTask(serviceSentinelDispatchBus <-chan model.Monitor) {
	for task := range serviceSentinelDispatchBus {
		for _, server := range singleton.SnapshotSortedServers() {
			if server.TaskStream == nil { continue }
			if (task.Cover == model.MonitorCoverAll && !task.SkipServers[server.ID]) ||
				(task.Cover == model.MonitorCoverIgnoreAll && task.SkipServers[server.ID]) {
				_ = singleton.SendTrackedTask(server,task.PB())
			}
		}
	}
}

func DispatchKeepalive() {
	singleton.Cron.AddFunc("@every 60s", func() {
		for _, server := range singleton.SnapshotSortedServers() {
			if server.TaskStream == nil { continue }
			_ = server.TaskStream.Send(&pb.Task{Type:model.TaskTypeKeepalive})
		}
	})
}

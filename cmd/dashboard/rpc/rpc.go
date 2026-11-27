package rpc

import (
	"fmt"
 "context"
	"net"

	"google.golang.org/grpc"

	"github.com/naiba/nezha/model"
 upgrade "github.com/naiba/nezha/service/agentupgrade"
 wire "github.com/naiba/nezha/pkg/upgradewire"
	pb "github.com/naiba/nezha/proto"
	rpcService "github.com/naiba/nezha/service/rpc"
	"github.com/naiba/nezha/service/singleton"
 "github.com/naiba/nezha/service/upgradecontrol"
)

func ServeRPC(port uint) {
	server := grpc.NewServer()
	rpcService.NezhaHandlerSingleton = rpcService.NewNezhaHandler()
	pb.RegisterNezhaServiceServer(server, rpcService.NezhaHandlerSingleton)
 endpoint,err:=singleton.Conf.GRPCListenAddress(port,upgradecontrol.Current!=nil)
 if err!=nil{panic(err)}
 handler:=&upgrade.RPC{}
 if c:=upgradecontrol.Current;c!=nil{
  // A configured managed upgrade deployment requires local TLS termination.
  handler=&upgrade.RPC{EnrollAgent:func(ctx context.Context)(uint64,[]byte,error){
   id,err:=rpcService.NezhaHandlerSingleton.Auth.Check(ctx);if err!=nil{return 0,nil,err}
   key,err:=c.Enroll(ctx,id);return id,key,err
  },Scheduler:c.Scheduler,AllowExecution:c.Execution,AllowLoopbackProxy:true,Authenticate:func(ctx context.Context)(uint64,error){
   id,err:=rpcService.NezhaHandlerSingleton.Auth.Check(ctx)
   if err!=nil||!c.AgentAllowed(id){return 0,fmt.Errorf("managed Agent denied")}
   return id,nil
  }}
 }
 wire.Register(server,handler)
 listen, err := net.Listen("tcp",endpoint)
	if err != nil {
		panic(err)
	}
	if err:=server.Serve(listen);err!=nil{panic(err)}
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

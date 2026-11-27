package agentupgrade

import (
 "context"
 "errors"
 "net"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
 wire "github.com/naiba/nezha/pkg/upgradewire"
 "google.golang.org/grpc"
 "google.golang.org/grpc/codes"
 "google.golang.org/grpc/credentials"
 "google.golang.org/grpc/peer"
 "google.golang.org/grpc/status"
 "google.golang.org/protobuf/types/known/wrapperspb"
 "gorm.io/gorm"
)

// RPC exposes the managed upgrade transport. A nil scheduler is disabled.
type RPC struct{
 Scheduler *Scheduler
 Authenticate func(context.Context)(uint64,error)
 // Only for a TLS-terminating local reverse proxy with protected backend.
 AllowLoopbackProxy bool
 // Separate explicit gate; disabled until production qualification.
 AllowExecution bool
}
func(r *RPC) identity(ctx context.Context)(uint64,error){
 if r.Scheduler==nil||!r.Scheduler.config.Enabled||r.Authenticate==nil{return 0,status.Error(codes.Unavailable,"managed upgrade disabled")}
 p,ok:=peer.FromContext(ctx)
 if !ok{return 0,status.Error(codes.PermissionDenied,"missing transport peer")}
 _,tlsOK:=p.AuthInfo.(credentials.TLSInfo)
 if !tlsOK{
  addr,ok:=p.Addr.(*net.TCPAddr)
  if !r.AllowLoopbackProxy||!ok||!addr.IP.IsLoopback(){return 0,status.Error(codes.PermissionDenied,"protected transport required")}
 }
 id,err:=r.Authenticate(ctx)
 if err!=nil||id==0{return 0,status.Error(codes.Unauthenticated,"Agent authentication failed")}
 return id,nil
}
func(r *RPC) Download(req *wrapperspb.BytesValue,stream grpc.ServerStream)error{
 ctx:=stream.Context()
 id,err:=r.identity(ctx)
 if err!=nil{return err}
 if req==nil||len(req.Value)!=0{return status.Error(codes.InvalidArgument,"invalid download request")}
 deadline,ok:=ctx.Deadline()
 if !ok||time.Until(deadline)>wire.MaxDownloadTime+time.Second{return status.Error(codes.InvalidArgument,"bounded deadline required")}
 var row taskRow
 err=r.Scheduler.store.db.WithContext(ctx).First(&row,"active_server_id = ?",id).Error
 if errors.Is(err,gorm.ErrRecordNotFound){return status.Error(codes.NotFound,"no pending upgrade")}
 if err!=nil{return status.Error(codes.Internal,"upgrade lookup failed")}
 record,err:=decode(row)
 if err!=nil||record.Task.State!=core.StatePending{return status.Error(codes.FailedPrecondition,"upgrade not pending")}
 tr,err:=r.Scheduler.BeginTransfer(ctx,id,record.Task.ID)
 if err!=nil{return status.Error(codes.FailedPrecondition,"upgrade claim refused")}
 defer tr.Close()
 header,err:=wire.EncodeHeader(wire.Header{Task:tr.Record.Task,Size:tr.Size})
 if err!=nil{return status.Error(codes.Internal,"invalid download header")}
 if err:=stream.SendMsg(wrapperspb.Bytes(header));err!=nil{return err}
 for offset:=int64(0);;{
  chunk,done,err:=tr.ChunkAt(ctx,offset)
  if err!=nil{return status.Error(codes.Aborted,"transfer interrupted")}
  if done{break}
  b,err:=wire.EncodeChunk(chunk)
  if err!=nil{return status.Error(codes.Internal,"invalid chunk")}
  if err:=stream.SendMsg(wrapperspb.Bytes(b));err!=nil{return err}
  offset+=int64(len(chunk.Data))
 }
 // A successful EOF is emitted only after a durable delivery marker.
 // Failed streams stay reserved for explicit reconciliation; never guess
 // that the Agent has not received the bytes.
 if err:=r.Scheduler.store.markDelivered(ctx,tr.Record);err!=nil{return status.Error(codes.Aborted,"delivery state unavailable")}
 return nil
}
func(r *RPC) Verify(ctx context.Context,req *wrapperspb.BytesValue)(*wrapperspb.BytesValue,error){
 id,err:=r.identity(ctx)
 if err!=nil{return nil,err}
 if req==nil{return nil,status.Error(codes.InvalidArgument,"missing verification receipt")}
 t,err:=core.DecodeSnapshot(req.Value)
 if err!=nil||t.State!=core.StateVerified||t.ServerID!=id{return nil,status.Error(codes.PermissionDenied,"invalid receipt identity")}
 if err:=r.Scheduler.store.ackVerified(ctx,t,time.Now());err!=nil{return nil,status.Error(codes.FailedPrecondition,"verification receipt refused")}
 return wrapperspb.Bytes([]byte("verified")),nil
}

// Compile-time registration contract.
var _ wire.Server=(*RPC)(nil)

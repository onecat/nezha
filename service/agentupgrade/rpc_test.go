package agentupgrade

import (
 "bytes"
 "context"
 "io"
 "net"
 "testing"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
 wire "github.com/naiba/nezha/pkg/upgradewire"
 "google.golang.org/grpc"
 "google.golang.org/grpc/credentials/insecure"
 "google.golang.org/grpc/metadata"
 "google.golang.org/protobuf/types/known/wrapperspb"
)

func TestRPCDownloadAndVerifiedReceipt(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
 defer cancel()
 store,p,e:=schedulerFixture(t)
 v:=importFixture(t,p,"linux","amd64")
 s,err:=NewScheduler(store,p,e,SchedulerConfig{Enabled:true,LinuxAMD64:true})
 if err!=nil{t.Fatal(err)}
 r,err:=s.Admit(ctx,9,1,v.PackageSHA256)
 if err!=nil{t.Fatal(err)}
 l,err:=net.Listen("tcp","127.0.0.1:0")
 if err!=nil{t.Fatal(err)}
 defer l.Close()
 g:=grpc.NewServer()
 wire.Register(g,&RPC{Scheduler:s,AllowLoopbackProxy:true,Authenticate:func(c context.Context)(uint64,error){
  m,_:=metadata.FromIncomingContext(c)
  if len(m.Get("test-id"))==1&&m.Get("test-id")[0]=="one"{return 1,nil}
  return 0,nil
 }})
 go g.Serve(l)
 defer g.Stop()
 conn,err:=grpc.NewClient("passthrough:///"+l.Addr().String(),grpc.WithTransportCredentials(insecure.NewCredentials()))
 if err!=nil{t.Fatal(err)}
 defer conn.Close()
 denied,err:=wire.Download(ctx,conn)
 if err!=nil{t.Fatal(err)}
 if err:=denied.RecvMsg(new(wrapperspb.BytesValue));err==nil{t.Fatal("unauthenticated transfer")}
 auth:=metadata.AppendToOutgoingContext(ctx,"test-id","one")
 premature:=r.Task;premature.State=core.StateVerified
 if err:=wire.Verify(auth,conn,premature);err==nil{t.Fatal("premature receipt")}
 stream,err:=wire.Download(auth,conn)
 if err!=nil{t.Fatal(err)}
 msg:=new(wrapperspb.BytesValue)
 if err:=stream.RecvMsg(msg);err!=nil{t.Fatal(err)}
 header,err:=wire.DecodeHeader(msg.Value)
 if err!=nil||!header.Task.SameIdentity(r.Task){t.Fatal("header mismatch",err)}
 var out bytes.Buffer
 recv,err:=core.NewReceiver(header.Task.ID,header.Task.ServerID,header.Task.PackageSHA256,header.Size,&out)
 if err!=nil{t.Fatal(err)}
 for{
  msg=new(wrapperspb.BytesValue)
  err:=stream.RecvMsg(msg)
  if err==io.EOF{break}
  if err!=nil{t.Fatal(err)}
  chunk,err:=wire.DecodeChunk(header,msg.Value)
  if err!=nil{t.Fatal(err)}
  if err:=recv.WriteChunk(chunk);err!=nil{t.Fatal(err)}
 }
 if err:=recv.Finish();err!=nil{t.Fatal(err)}
 if _,err:=core.VerifyPackage(bytes.NewReader(out.Bytes()),int64(out.Len()),p.keys);err!=nil{t.Fatal(err)}
 verified,err:=header.Task.Transition(core.StateVerified,time.Now())
 if err!=nil{t.Fatal(err)}
 wrong:=verified;wrong.ServerID=2
 if err:=wire.Verify(auth,conn,wrong);err==nil{t.Fatal("wrong identity accepted")}
 if err:=wire.Verify(auth,conn,verified);err!=nil{t.Fatal(err)}
 if err:=wire.Verify(auth,conn,verified);err!=nil{t.Fatal("idempotent verify",err)}
 got,err:=store.Get(ctx,r.Task.ID)
 if err!=nil||got.Task.State!=core.StateVerified{t.Fatal("receipt not persisted",err)}
}

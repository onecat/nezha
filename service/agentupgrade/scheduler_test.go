package agentupgrade

import (
 "bytes"
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "errors"
 "os"
 "path/filepath"
 "runtime"
 "testing"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
)

type eligibilityFixture struct{
 policy core.AuthorizationPolicy
 high uint64
 operator uint64
}
func(e *eligibilityFixture)Policy(_ context.Context,operator,server uint64)(core.AuthorizationPolicy,uint64,error){
 if operator!=e.operator||server==0{return core.AuthorizationPolicy{},0,errors.New("denied")}
 return e.policy,e.high,nil
}
func schedulerFixture(t *testing.T)(*Store,*Packages,*eligibilityFixture){
 t.Helper()
 if runtime.GOOS!="linux"{t.Skip("Linux package store")}
 s:=openTest(t,filepath.Join(t.TempDir(),"tasks.db"))
 dir:=t.TempDir()
 if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)}
 pub,_,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 // The actual package test provisions its own signing key.
 p,err:=OpenPackages(dir,map[string]ed25519.PublicKey{"placeholder":pub})
 if err!=nil{t.Fatal(err)}
 e:=&eligibilityFixture{operator:9,high:1,policy:core.AuthorizationPolicy{Enabled:true,OperatorCanUpgrade:true,ServerAllowsUpgrade:true,AgentSupportsUpgrade:true,AgentVersion:"v0.30.00",AgentOS:"linux",AgentArch:"amd64"}}
 return s,p,e
}
func importFixture(t *testing.T,p *Packages,osName,arch string)core.VerifiedPackage{
 t.Helper()
 pub,priv,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 p.keys["test"]=pub
 var b bytes.Buffer
 body:=[]byte("fixture only")
 m:=core.Manifest{Version:"v0.30.01",MinAgentVersion:"v0.30.00",OS:osName,Arch:arch,SigningKeyID:"test",ReleaseSequence:2}
 if _,err:=core.BuildPackage(&b,bytes.NewReader(body),int64(len(body)),m,priv);err!=nil{t.Fatal(err)}
 v,err:=p.Import(context.Background(),bytes.NewReader(b.Bytes()))
 if err!=nil{t.Fatal(err)}
 return v
}
func TestSchedulerAdmissionAndTransfer(t *testing.T){
 ctx:=context.Background()
 store,p,e:=schedulerFixture(t)
 v:=importFixture(t,p,"linux","amd64")
 disabled,err:=NewScheduler(store,p,e,SchedulerConfig{})
 if err!=nil{t.Fatal(err)}
 if _,err:=disabled.Admit(ctx,9,1,v.PackageSHA256);err==nil{t.Fatal("enabled by default")}
 unqualified,err:=NewScheduler(store,p,e,SchedulerConfig{Enabled:true})
 if err!=nil{t.Fatal(err)}
 if _,err:=unqualified.Admit(ctx,9,1,v.PackageSHA256);err==nil{t.Fatal("unqualified platform accepted")}
 s,err:=NewScheduler(store,p,e,SchedulerConfig{Enabled:true,LinuxAMD64:true})
 if err!=nil{t.Fatal(err)}
 if _,err:=s.Admit(ctx,10,1,v.PackageSHA256);err==nil{t.Fatal("wrong operator")}
 r,err:=s.Admit(ctx,9,1,v.PackageSHA256)
 if err!=nil{t.Fatal(err)}
 if _,err:=s.Admit(ctx,9,1,v.PackageSHA256);err==nil{t.Fatal("duplicate active target")}
 if _,err:=s.BeginTransfer(ctx,2,r.Task.ID);err==nil{t.Fatal("wrong session identity")}
 e.policy.OperatorCanUpgrade=false
 if _,err:=s.BeginTransfer(ctx,1,r.Task.ID);err==nil{t.Fatal("revoked authorization accepted")}
 e.policy.OperatorCanUpgrade=true
 tr,err:=s.BeginTransfer(ctx,1,r.Task.ID)
 if err!=nil{t.Fatal(err)}
 defer tr.Close()
 if tr.Record.Task.State!=core.StateTransferring{t.Fatal("not claimed")}
 if _,err:=s.BeginTransfer(ctx,1,r.Task.ID);err==nil{t.Fatal("duplicate claim")}
 var buf bytes.Buffer
 receiver,err:=core.NewReceiver(r.Task.ID,1,v.PackageSHA256,tr.Size,&buf)
 if err!=nil{t.Fatal(err)}
 var offset int64
 for{
  c,done,err:=tr.ChunkAt(ctx,offset)
  if err!=nil{t.Fatal(err)}
  if done{break}
  if err:=receiver.WriteChunk(c);err!=nil{t.Fatal(err)}
  offset+=int64(len(c.Data))
 }
 if err:=receiver.Finish();err!=nil{t.Fatal(err)}
 if _,err:=core.VerifyPackage(bytes.NewReader(buf.Bytes()),int64(buf.Len()),p.keys);err!=nil{t.Fatal(err)}
 if _,err:=s.Admit(ctx,9,2,v.PackageSHA256);err!=nil{t.Fatal(err)}
 if _,err:=s.Admit(ctx,9,3,v.PackageSHA256);err!=nil{t.Fatal(err)}
 if _,err:=s.Admit(ctx,9,4,v.PackageSHA256);!errors.Is(err,ErrCapacity){t.Fatalf("capacity: %v",err)}
 if _,err:=store.Advance(ctx,tr.Record,core.StateFailed,time.Now());err!=nil{t.Fatal(err)}
 if _,err:=s.Admit(ctx,9,4,v.PackageSHA256);err!=nil{t.Fatal(err)}
}
func TestSchedulerRejectWindowsAndUnauditedTasks(t *testing.T){
 ctx:=context.Background()
 store,p,e:=schedulerFixture(t)
 v:=importFixture(t,p,"windows","amd64")
 e.policy.AgentOS="windows"
 s,err:=NewScheduler(store,p,e,SchedulerConfig{Enabled:true,LinuxAMD64:true,LinuxARM64:true})
 if err!=nil{t.Fatal(err)}
 if _,err:=s.Admit(ctx,9,1,v.PackageSHA256);err==nil{t.Fatal("Windows upgrade accepted")}
 r,err:=store.Create(ctx,fixture("unaudited",1,time.Now()),time.Now())
 if err!=nil{t.Fatal(err)}
 if _,err:=s.BeginTransfer(ctx,1,r.Task.ID);err==nil{t.Fatal("unaudited task accepted")}
}

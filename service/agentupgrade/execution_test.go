package agentupgrade
import (
 "context"
 "testing"
 "time"
 core "github.com/naiba/nezha/pkg/agentupgrade"
)
func TestGrantAndTerminalReceipt(t *testing.T){
 ctx:=context.Background()
 for _,terminal:=range []core.State{core.StateSucceeded,core.StateRolledBack,core.StateFailed}{
  t.Run(string(terminal),func(t *testing.T){
   store,p,e:=schedulerFixture(t)
   pkg:=importFixture(t,p,"linux","amd64")
   s,err:=NewScheduler(store,p,e,SchedulerConfig{Enabled:true,LinuxAMD64:true})
   if err!=nil{t.Fatal(err)}
   admitted,err:=s.Admit(ctx,9,1,pkg.PackageSHA256)
   if err!=nil{t.Fatal(err)}
   tr,err:=s.BeginTransfer(ctx,1,admitted.Task.ID)
   if err!=nil{t.Fatal(err)}
   defer tr.Close()
   if err:=store.markDelivered(ctx,tr.Record);err!=nil{t.Fatal(err)}
   verified,err:=tr.Record.Task.Transition(core.StateVerified,time.Now())
   if err!=nil{t.Fatal(err)}
   if err:=store.ackVerified(ctx,verified,time.Now());err!=nil{t.Fatal(err)}
   result:=verified;result.State=terminal
   premature:=result;premature.State=core.StateSucceeded
   if err:=store.recordResult(ctx,premature,time.Now());err==nil{t.Fatal("success before grant accepted")}
   e.policy.OperatorCanUpgrade=false
   if err:=s.grant(ctx,verified);err==nil{t.Fatal("revoked grant accepted")}
   e.policy.OperatorCanUpgrade=true
   if err:=s.grant(ctx,verified);err!=nil{t.Fatal(err)}
   if err:=s.grant(ctx,verified);err==nil{t.Fatal("replayed grant accepted")}
   wrong:=result;wrong.ServerID=2
   if err:=store.recordResult(ctx,wrong,time.Now());err==nil{t.Fatal("wrong result accepted")}
   if err:=store.recordResult(ctx,result,time.Now());err!=nil{t.Fatal(err)}
   if err:=store.recordResult(ctx,result,time.Now());err!=nil{t.Fatal("retry",err)}
   got,err:=store.Get(ctx,result.ID)
   if err!=nil||got.Task.State!=terminal{t.Fatal("missing result",err)}
   if _,err:=s.Admit(ctx,9,1,pkg.PackageSHA256);err!=nil{t.Fatal("reservation not released",err)}
  })
 }
}

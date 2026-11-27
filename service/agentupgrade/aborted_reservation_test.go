package agentupgrade

import (
 "context"
 "testing"
 "time"
 core "github.com/naiba/nezha/pkg/agentupgrade"
)
func TestAbortedReservationReconcilesWithoutGrant(t *testing.T){
 ctx:=context.Background()
 for _,expired:=range []bool{false,true}{
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
  if expired{
   rec,err:=store.Get(ctx,verified.ID)
   if err!=nil{t.Fatal(err)}
   if _,err:=store.Advance(ctx,rec,core.StateFailed,verified.ExpiresAt.Add(time.Second));err!=nil{t.Fatal(err)}
  }
  failed:=verified;failed.State=core.StateFailed
  wrong:=failed;wrong.ServerID=2
  if err:=store.recordResult(ctx,wrong,time.Now());err==nil{t.Fatal("wrong target accepted")}
  if err:=store.recordResult(ctx,failed,time.Now());err!=nil{t.Fatal(err)}
  if err:=store.recordResult(ctx,failed,time.Now());err!=nil{t.Fatal("idempotent retry",err)}
  if err:=s.grant(ctx,verified);err==nil{t.Fatal("grant after abort")}
  high,err:=store.HighWater(ctx,1,1)
  if err!=nil||high!=2{t.Fatal("consumed sequence lost",high,err)}
  rec,err:=store.Get(ctx,failed.ID)
  if err!=nil||rec.Task.State!=core.StateFailed{t.Fatal("not terminal",err)}
  e.high=high
  if _,err:=s.Admit(ctx,9,1,pkg.PackageSHA256);err==nil{t.Fatal("consumed release readmitted")}
 }
}

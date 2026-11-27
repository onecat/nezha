package agentupgrade

import (
 "context"
 "errors"
 "testing"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
)

func TestLateSuccessRequiresExplicitReview(t *testing.T){
 ctx:=context.Background()
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
 if err:=s.grant(ctx,verified);err!=nil{t.Fatal(err)}
 // Use an injected late observation time without waiting for the 15-minute
 // expiry. The review's clock check is exercised by a separate helper below.
 late:=verified;late.State=core.StateSucceeded
 observed:=verified.ExpiresAt.Add(time.Second)
 if err:=store.recordResult(ctx,late,observed);!errors.Is(err,ErrLateSuccessReview){t.Fatal("late result not quarantined",err)}
 if err:=store.recordResult(ctx,late,observed);!errors.Is(err,ErrLateSuccessReview){t.Fatal("late retry not quarantined",err)}
 detail,err:=s.StatusDetail(ctx,late.ID)
 if err!=nil||detail.Task.State!=core.StateInstalling||detail.LateSuccessAt==nil||detail.ReviewedAt!=nil{t.Fatal("invalid review status",err)}
 wrong:=late;wrong.State=core.StateFailed
 if err:=store.recordResult(ctx,wrong,observed);err==nil{t.Fatal("conflicting result accepted")}
 if _,err:=s.ReconcileLateSuccess(ctx,9,late.ID,detail.Revision+1,late.PackageSHA256);err==nil{t.Fatal("stale revision accepted")}
 if _,err:=s.ReconcileLateSuccess(ctx,9,late.ID,detail.Revision,late.PackageSHA256);err==nil{t.Fatal("wrong current version accepted")}
 e.policy.AgentVersion=late.Version;e.high=2
 if _,err:=s.ReconcileLateSuccess(ctx,10,late.ID,detail.Revision,late.PackageSHA256);err==nil{t.Fatal("unauthorized reviewer")}
 if _,err:=s.ReconcileLateSuccess(ctx,9,late.ID,detail.Revision,late.PackageSHA256);err==nil{t.Fatal("future observation accepted")}
 reviewed,err:=s.reconcileLateSuccess(ctx,9,late.ID,detail.Revision,late.PackageSHA256,observed.Add(time.Second))
 if err!=nil||reviewed.Task.State!=core.StateSucceeded{t.Fatal("review failed",err)}
 if err:=store.recordResult(ctx,late,observed.Add(2*time.Second));err!=nil{t.Fatal("reviewed receipt retry",err)}
 detail,err=s.StatusDetail(ctx,late.ID)
 if err!=nil||detail.ReviewedBy!=9||detail.ReviewedAt==nil{t.Fatal("missing audit",err)}
 if _,err:=s.AdmitWave(ctx,9,[]uint64{2},late.PackageSHA256,late.ID);err==nil{t.Fatal("reviewed late result used as canary")}
}

package agentupgrade

import (
 "context"
 "errors"
 "io"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
)

// HighWater derives a conservative Dashboard floor from immutable grants and aborted reservations.
// The independently provisioned Agent ledger remains authoritative.
func(s *Store) HighWater(ctx context.Context,server,initial uint64)(uint64,error){
 if server==0||initial==0{return 0,errors.New("missing initial sequence")}
 var grants []executionRow
 err:=s.db.WithContext(ctx).Table("managed_agent_upgrade_executions").
  Joins("JOIN managed_agent_upgrade_tasks ON managed_agent_upgrade_tasks.id = managed_agent_upgrade_executions.task_id").
  Where("managed_agent_upgrade_tasks.server_id = ?",server).
  Select("managed_agent_upgrade_executions.*").Limit(10001).Find(&grants).Error
 if err!=nil{return 0,err}
 if len(grants)>10000{return 0,errors.New("grant history requires maintenance")}
 high:=initial
 for _,g:=range grants{
  t,err:=core.DecodeSnapshot(g.Grant)
  if err!=nil||t.ServerID!=server||t.ID!=g.TaskID||t.State!=core.StateInstalling{return 0,errors.New("invalid grant history")}
  if t.ReleaseSequence>high{high=t.ReleaseSequence}
 }
 var aborted []abortedReservationRow
 err=s.db.WithContext(ctx).Table("managed_agent_upgrade_aborted_reservations").
  Joins("JOIN managed_agent_upgrade_tasks ON managed_agent_upgrade_tasks.id = managed_agent_upgrade_aborted_reservations.task_id").
  Where("managed_agent_upgrade_tasks.server_id = ?",server).
  Select("managed_agent_upgrade_aborted_reservations.*").Limit(10001).Find(&aborted).Error
 if err!=nil{return 0,err}
 if len(aborted)>10000{return 0,errors.New("aborted reservation history requires maintenance")}
 for _,a:=range aborted{
  t,err:=core.DecodeSnapshot(a.Snapshot)
  if err!=nil||t.ServerID!=server||t.ID!=a.TaskID||t.State!=core.StateFailed||a.RecordedAt.IsZero(){return 0,errors.New("invalid aborted reservation history")}
  if t.ReleaseSequence>high{high=t.ReleaseSequence}
 }
 return high,nil
}
func(s *Scheduler) Import(ctx context.Context,r io.Reader)(core.VerifiedPackage,error){
 if !s.config.Enabled{return core.VerifiedPackage{},errors.New("upgrade disabled")}
 return s.packages.Import(ctx,r)
}
func(s *Scheduler) Status(ctx context.Context,id string)(Record,error){return s.store.Get(ctx,id)}
// AdmitWave is deliberately operator-driven: one canary first, then at
// most three targets per explicitly requested wave. No autonomous rollout.
// Each target is independently authorized; partial admissions are reported.
func(s *Scheduler) AdmitWave(ctx context.Context,operator uint64,servers []uint64,digest,canaryID string)([]Record,error){
 if len(servers)==0||len(servers)>MaxActiveTasks{return nil,errors.New("select one to three targets")}
 seen:=map[uint64]bool{}
 for _,id:=range servers{if id==0||seen[id]{return nil,errors.New("invalid or duplicate target")};seen[id]=true}
 if canaryID==""&&len(servers)!=1{return nil,errors.New("successful canary required")}
 if canaryID!=""{
  r,err:=s.store.Get(ctx,canaryID)
  if err!=nil||r.Task.State!=core.StateSucceeded||r.Task.PackageSHA256!=digest||seen[r.Task.ServerID]{return nil,errors.New("canary not successful or package mismatch")}
  var audit admissionRow
  if err:=s.store.db.WithContext(ctx).First(&audit,"task_id = ?",canaryID).Error;err!=nil||audit.OperatorID!=operator{return nil,errors.New("canary operator mismatch")}
  var execution executionRow
  if err:=s.store.db.WithContext(ctx).First(&execution,"task_id = ?",canaryID).Error;err!=nil{return nil,err}
  result,err:=core.DecodeSnapshot(execution.Result)
  if err!=nil||!result.SameIdentity(r.Task)||result.State!=core.StateSucceeded||execution.ResultAt==nil||execution.LateResultAt!=nil{return nil,errors.New("canary result unavailable")}
 }
 out:=make([]Record,0,len(servers))
 for _,id:=range servers{
  r,err:=s.Admit(ctx,operator,id,digest)
  if err!=nil{return out,err}
  out=append(out,r)
 }
 return out,nil
}
// ExpireUnstarted releases only tasks for which no installation grant
// could have been issued. Installing tasks always need reconciliation.
func(s *Scheduler) ExpireUnstarted(ctx context.Context)error{
 records,err:=s.store.ActivePage(ctx,"",MaxActiveTasks)
 if err!=nil{return err}
 for _,r:=range records{
  if time.Now().Before(r.Task.ExpiresAt){continue}
  switch r.Task.State{
  case core.StatePending,core.StateTransferring,core.StateVerified:
   if _,err:=s.store.Advance(ctx,r,core.StateFailed,time.Now());err!=nil{return err}
  }
 }
 return nil
}

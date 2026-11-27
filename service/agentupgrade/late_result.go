package agentupgrade

import (
 "context"
 "errors"
 "math"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
 "gorm.io/gorm"
)

var ErrLateSuccessReview=errors.New("late success requires operator review")

// Persist the exact authenticated late observation without releasing the
// reservation or acknowledging success. A retry cannot change its contents.
func(s *Store) recordLateSuccess(ctx context.Context,r Record,t core.Transaction,b []byte,now time.Time)error{
 if r.Task.State!=core.StateInstalling||t.State!=core.StateSucceeded||now.Before(t.ExpiresAt){return ErrConflict}
 before,err:=core.EncodeSnapshot(r.Task)
 if err!=nil{return err}
 err=s.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  var row taskRow
  if err:=tx.First(&row,"id = ? AND revision = ? AND snapshot = ?",t.ID,r.Revision,before).Error;err!=nil{return err}
  res:=tx.Model(&executionRow{}).Where("task_id = ? AND result IS NULL AND late_result IS NULL",t.ID).
   Updates(map[string]any{"late_result":b,"late_result_at":now.UTC()})
  if res.Error!=nil{return res.Error}
  if res.RowsAffected!=1{return ErrConflict}
  return nil
 })
 if err!=nil{return err}
 return ErrLateSuccessReview
}

type StatusDetail struct{
 Record
 LateSuccessAt *time.Time
 ReviewedBy uint64
 ReviewedAt *time.Time
}

// StatusDetail contains no secrets or raw execution audit blobs.
func(s *Scheduler) StatusDetail(ctx context.Context,id string)(StatusDetail,error){
 r,err:=s.store.Get(ctx,id)
 if err!=nil{return StatusDetail{},err}
 detail:=StatusDetail{Record:r}
 var e executionRow
 err=s.store.db.WithContext(ctx).First(&e,"task_id = ?",id).Error
 if errors.Is(err,gorm.ErrRecordNotFound){return detail,nil}
 if err!=nil{return StatusDetail{},err}
 if len(e.LateResult)!=0{
  claim,err:=core.DecodeSnapshot(e.LateResult)
  if err!=nil||claim.State!=core.StateSucceeded||!claim.SameIdentity(r.Task)||!claim.ExpiresAt.Equal(r.Task.ExpiresAt)||e.LateResultAt==nil{return StatusDetail{},ErrConflict}
  detail.LateSuccessAt=e.LateResultAt
  detail.ReviewedBy=e.ReviewedBy
  detail.ReviewedAt=e.ReviewedAt
 }
 return detail,nil
}

// ReconcileLateSuccess is an explicit operator confirmation, never a retry
// of installation. The authenticated operator must first inspect the exact
// local ledger/binary. Current eligibility and reported version are an
// additional check, not independent remote attestation.
func(s *Scheduler) ReconcileLateSuccess(ctx context.Context,operator uint64,id string,revision int64,digest string)(Record,error){
 return s.reconcileLateSuccess(ctx,operator,id,revision,digest,time.Now().UTC())
}
func(s *Scheduler) reconcileLateSuccess(ctx context.Context,operator uint64,id string,revision int64,digest string,now time.Time)(Record,error){
 if !s.config.Enabled||operator==0||operator>math.MaxInt64||now.IsZero(){return Record{},ErrConflict}
 r,err:=s.store.Get(ctx,id)
 if err!=nil{return Record{},err}
 if r.Revision!=revision||r.Task.PackageSHA256!=digest||r.Task.State!=core.StateInstalling||r.Revision==math.MaxInt64{return Record{},ErrConflict}
 var e executionRow
 if err:=s.store.db.WithContext(ctx).First(&e,"task_id = ?",id).Error;err!=nil{return Record{},err}
 if len(e.Result)!=0||e.LateResultAt==nil||e.ReviewedAt!=nil||e.ReviewedBy!=0{return Record{},ErrConflict}
 claim,err:=core.DecodeSnapshot(e.LateResult)
 if err!=nil||claim.State!=core.StateSucceeded||!claim.SameIdentity(r.Task)||!claim.ExpiresAt.Equal(r.Task.ExpiresAt){return Record{},ErrConflict}
 grant,err:=core.DecodeSnapshot(e.Grant)
 if err!=nil||grant.State!=core.StateInstalling||!grant.SameIdentity(r.Task)||!grant.ExpiresAt.Equal(r.Task.ExpiresAt)||e.GrantedAt.IsZero()||!e.GrantedAt.Before(grant.ExpiresAt)||e.LateResultAt.Before(grant.ExpiresAt){return Record{},ErrConflict}
 p,high,err:=s.eligibility.Policy(ctx,operator,r.Task.ServerID)
 if err!=nil{return Record{},err}
 if !p.Enabled||!p.OperatorCanUpgrade||!p.ServerAllowsUpgrade||!p.AgentSupportsUpgrade||p.AgentOS!="linux"||
  !((p.AgentArch=="amd64"&&s.config.LinuxAMD64)||(p.AgentArch=="arm64"&&s.config.LinuxARM64))||p.AgentVersion!=claim.Version||high<claim.ReleaseSequence{return Record{},errors.New("current authenticated target version or eligibility mismatch")}
 before,err:=core.EncodeSnapshot(r.Task)
 if err!=nil{return Record{},err}
 now=now.UTC()
 if now.Before(claim.ExpiresAt)||now.Before(*e.LateResultAt){return Record{},ErrConflict}
 err=s.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  res:=tx.Model(&taskRow{}).Where("id = ? AND revision = ? AND snapshot = ?",id,r.Revision,before).
   Updates(map[string]any{"revision":r.Revision+1,"snapshot":e.LateResult,"active_server_id":nil})
  if res.Error!=nil{return res.Error}
  if res.RowsAffected!=1{return ErrConflict}
  res=tx.Model(&executionRow{}).Where("task_id = ? AND result IS NULL AND late_result = ? AND reviewed_at IS NULL",id,e.LateResult).
   Updates(map[string]any{"result":e.LateResult,"result_at":now,"reviewed_by":operator,"reviewed_at":now})
  if res.Error!=nil{return res.Error}
  if res.RowsAffected!=1{return ErrConflict}
  return nil
 })
 if err!=nil{return Record{},err}
 return Record{Task:claim,Revision:r.Revision+1},nil
}

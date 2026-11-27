package agentupgrade

import (
 "context"
 "errors"
 "math"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
 "google.golang.org/grpc/codes"
 "google.golang.org/grpc/status"
 "google.golang.org/protobuf/types/known/wrapperspb"
 "gorm.io/gorm"
)

type executionRow struct{
 TaskID string `gorm:"primaryKey;size:128;not null"`
 GrantedAt time.Time `gorm:"not null"`
 Grant []byte `gorm:"not null"`
 Result []byte
 ResultAt *time.Time
}
func(executionRow)TableName()string{return "managed_agent_upgrade_executions"}

func(s *Scheduler) grant(ctx context.Context,t core.Transaction)error{
 r,err:=s.store.Get(ctx,t.ID)
 if err!=nil{return err}
 if t.State!=core.StateVerified||r.Task.State!=core.StateVerified||!r.Task.SameIdentity(t)||!r.Task.ExpiresAt.Equal(t.ExpiresAt){return ErrConflict}
 var audit admissionRow
 if err:=s.store.db.WithContext(ctx).First(&audit,"task_id = ?",t.ID).Error;err!=nil{return err}
 original,err:=core.DecodeSnapshot(audit.Snapshot)
 if err!=nil||original.State!=core.StatePending||!original.SameIdentity(t)||!original.ExpiresAt.Equal(t.ExpiresAt)||audit.OperatorID==0{return errors.New("invalid approval")}
 f,p,err:=s.packages.Open(t.PackageSHA256)
 if err!=nil{return err}
 defer f.Close()
 if err:=s.authorize(ctx,audit.OperatorID,original,p);err!=nil{return err}
 now:=time.Now()
 next,err:=r.Task.Transition(core.StateInstalling,now)
 if err!=nil{return err}
 before,err:=core.EncodeSnapshot(r.Task)
 if err!=nil{return err}
 after,err:=core.EncodeSnapshot(next)
 if err!=nil{return err}
 return s.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  res:=tx.Model(&taskRow{}).Where("id = ? AND revision = ? AND snapshot = ?",t.ID,r.Revision,before).Updates(map[string]any{"revision":r.Revision+1,"snapshot":after})
  if res.Error!=nil{return res.Error}
  if res.RowsAffected!=1{return ErrConflict}
  return tx.Create(&executionRow{TaskID:t.ID,GrantedAt:now.UTC(),Grant:after}).Error
 })
}

// recordResult persists the authenticated Agent's terminal claim and the
// resulting server state atomically. The Agent is responsible for local
// process-bound health proof. This is not independent remote attestation.
func(s *Store) recordResult(ctx context.Context,t core.Transaction,now time.Time)error{
 r,err:=s.Get(ctx,t.ID)
 if err!=nil{return err}
 if !r.Task.SameIdentity(t)||!r.Task.ExpiresAt.Equal(t.ExpiresAt){return ErrConflict}
 var exec executionRow
 if err:=s.db.WithContext(ctx).First(&exec,"task_id = ?",t.ID).Error;err!=nil{
  if errors.Is(err,gorm.ErrRecordNotFound)&&t.State==core.StateFailed{return s.recordAbortedReservation(ctx,r,t,now)}
  return err
 }
 grant,err:=core.DecodeSnapshot(exec.Grant)
 if err!=nil||grant.State!=core.StateInstalling||!grant.SameIdentity(t)||!grant.ExpiresAt.Equal(t.ExpiresAt)||exec.GrantedAt.IsZero(){return errors.New("invalid execution grant")}
 b,err:=core.EncodeSnapshot(t)
 if err!=nil{return err}
 if len(exec.Result)!=0{
  if string(exec.Result)==string(b)&&r.Task.State==t.State&&exec.ResultAt!=nil{return nil}
  return ErrConflict
 }
 if r.Task.State!=core.StateInstalling||r.Revision==math.MaxInt64{return ErrConflict}
 // Successful results must arrive before expiry. Late failures/rollbacks
 // remain reportable; an expired success requires operator reconciliation.
 switch t.State{
 case core.StateSucceeded:
  if !now.Before(t.ExpiresAt){return errors.New("late success requires reconciliation")}
 case core.StateFailed,core.StateRolledBack:
 default:return errors.New("nonterminal result")
 }
 before,err:=core.EncodeSnapshot(r.Task)
 if err!=nil{return err}
 return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  res:=tx.Model(&taskRow{}).Where("id = ? AND revision = ? AND snapshot = ?",t.ID,r.Revision,before).Updates(map[string]any{"revision":r.Revision+1,"snapshot":b,"active_server_id":nil})
  if res.Error!=nil{return res.Error}
  if res.RowsAffected!=1{return ErrConflict}
  res=tx.Model(&executionRow{}).Where("task_id = ? AND result IS NULL",t.ID).Updates(map[string]any{"result":b,"result_at":now.UTC()})
  if res.Error!=nil{return res.Error}
  if res.RowsAffected!=1{return ErrConflict}
  return nil
 })
}

func(r *RPC) executionTask(ctx context.Context,req *wrapperspb.BytesValue)(core.Transaction,error){
 id,err:=r.identity(ctx)
 if err!=nil{return core.Transaction{},err}
 if req==nil{return core.Transaction{},status.Error(codes.InvalidArgument,"missing task")}
 t,err:=core.DecodeSnapshot(req.Value)
 if err!=nil||t.ServerID!=id{return core.Transaction{},status.Error(codes.PermissionDenied,"invalid task identity")}
 return t,nil
}
func(r *RPC) Grant(ctx context.Context,req *wrapperspb.BytesValue)(*wrapperspb.BytesValue,error){
 t,err:=r.executionTask(ctx,req)
 if err!=nil{return nil,err}
 if !r.AllowExecution{return nil,status.Error(codes.Unavailable,"installation disabled")}
 if err:=r.Scheduler.grant(ctx,t);err!=nil{return nil,status.Error(codes.FailedPrecondition,"installation grant refused")}
 return wrapperspb.Bytes([]byte("installing")),nil
}
func(r *RPC) Result(ctx context.Context,req *wrapperspb.BytesValue)(*wrapperspb.BytesValue,error){
 t,err:=r.executionTask(ctx,req)
 if err!=nil{return nil,err}
 if err:=r.Scheduler.store.recordResult(ctx,t,time.Now());err!=nil{return nil,status.Error(codes.FailedPrecondition,"result refused")}
 return wrapperspb.Bytes([]byte(t.State)),nil
}

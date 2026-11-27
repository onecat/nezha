package agentupgrade

import (
 "context"
 "errors"
 "math"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
 "gorm.io/gorm"
)

// Records a consumed local sequence when no installation grant was issued.
// This is an authenticated terminal claim, never authorization to install.
type abortedReservationRow struct{
 TaskID string `gorm:"primaryKey;size:128;not null"`
 Snapshot []byte `gorm:"not null"`
 RecordedAt time.Time `gorm:"not null"`
}
func(abortedReservationRow)TableName()string{return "managed_agent_upgrade_aborted_reservations"}

func(s *Store) recordAbortedReservation(ctx context.Context,r Record,t core.Transaction,now time.Time)error{
 if t.State!=core.StateFailed||!r.Task.SameIdentity(t)||!r.Task.ExpiresAt.Equal(t.ExpiresAt)||now.IsZero()||
  (r.Task.State!=core.StateVerified&&r.Task.State!=core.StateFailed){return ErrConflict}
 b,err:=core.EncodeSnapshot(t)
 if err!=nil{return err}
 before,err:=core.EncodeSnapshot(r.Task)
 if err!=nil{return err}
 return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  // Both paths compete on the same immutable task snapshot/revision.
  // A grant winning the race is reconciled via the granted-result path.
  var current taskRow
  if err:=tx.First(&current,"id = ? AND revision = ? AND snapshot = ?",t.ID,r.Revision,before).Error;err!=nil{return err}
  var count int64
  if err:=tx.Model(&executionRow{}).Where("task_id = ?",t.ID).Count(&count).Error;err!=nil{return err}
  if count!=0{return ErrConflict}
  var existing abortedReservationRow
  err:=tx.First(&existing,"task_id = ?",t.ID).Error
  if err==nil{
   if existing.RecordedAt.IsZero()||string(existing.Snapshot)!=string(b)||r.Task.State!=core.StateFailed{return ErrConflict}
   return nil
  }
  if !errors.Is(err,gorm.ErrRecordNotFound){return err}
  // A verified receipt implies the complete archive was delivered. An
  // expired task may already be failed; still preserve its consumed floor.
  var d deliveryRow
  if err:=tx.First(&d,"task_id = ?",t.ID).Error;err!=nil{return err}
  sent,err:=core.DecodeSnapshot(d.Snapshot)
  if err!=nil||sent.State!=core.StateTransferring||!sent.SameIdentity(t)||!sent.ExpiresAt.Equal(t.ExpiresAt)||d.Revision<1||d.Revision>=r.Revision{return ErrConflict}
  if r.Revision==math.MaxInt64{return ErrConflict}
  res:=tx.Model(&taskRow{}).Where("id = ? AND revision = ? AND snapshot = ?",t.ID,r.Revision,before).
   Updates(map[string]any{"revision":r.Revision+1,"snapshot":b,"active_server_id":nil})
  if res.Error!=nil{return res.Error}
  if res.RowsAffected!=1{return ErrConflict}
  return tx.Create(&abortedReservationRow{TaskID:t.ID,Snapshot:b,RecordedAt:now.UTC()}).Error
 })
}

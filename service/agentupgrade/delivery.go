package agentupgrade

import (
 "context"
 "errors"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
 "gorm.io/gorm"
)

type deliveryRow struct{
 TaskID string `gorm:"primaryKey;size:128;not null"`
 Revision int64 `gorm:"not null"`
 Snapshot []byte `gorm:"not null"`
}
func(deliveryRow)TableName()string{return "managed_agent_upgrade_deliveries"}

func(s *Store) markDelivered(ctx context.Context,r Record)error{
 if r.Task.State!=core.StateTransferring||!time.Now().Before(r.Task.ExpiresAt){return errors.New("invalid delivery state")}
 b,err:=core.EncodeSnapshot(r.Task)
 if err!=nil{return err}
 return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  var row taskRow
  if err:=tx.First(&row,"id = ? AND revision = ? AND snapshot = ?",r.Task.ID,r.Revision,b).Error;err!=nil{return err}
  return tx.Create(&deliveryRow{TaskID:r.Task.ID,Revision:r.Revision,Snapshot:b}).Error
 })
}

// ackVerified records an authenticated claim of verification, not a grant
// to execute. It is idempotent only for the exact matching verified task.
func(s *Store) ackVerified(ctx context.Context,t core.Transaction,now time.Time)error{
 r,err:=s.Get(ctx,t.ID)
 if err!=nil{return err}
 if !r.Task.SameIdentity(t)||!r.Task.ExpiresAt.Equal(t.ExpiresAt)||!now.Before(t.ExpiresAt)||t.State!=core.StateVerified{return errors.New("receipt mismatch")}
 var d deliveryRow
 if err:=s.db.WithContext(ctx).First(&d,"task_id = ?",t.ID).Error;err!=nil{return err}
 sent,err:=core.DecodeSnapshot(d.Snapshot)
 if err!=nil||sent.State!=core.StateTransferring||!sent.SameIdentity(t)||!sent.ExpiresAt.Equal(t.ExpiresAt){return errors.New("invalid delivery marker")}
 if r.Task.State==core.StateVerified&&r.Revision==d.Revision+1{return nil}
 if r.Task.State!=core.StateTransferring||r.Revision!=d.Revision{return ErrConflict}
 _,err=s.Advance(ctx,r,core.StateVerified,now)
 return err
}

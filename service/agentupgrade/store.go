// Package agentupgrade provides Dashboard-side persistence. It exposes no
// HTTP/RPC handlers and never dispatches or executes a task.
package agentupgrade

import (
 "context"
 "errors"
 "fmt"
 "math"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
 "gorm.io/gorm"
)

var ErrConflict = errors.New("upgrade task revision conflict")
var ErrCapacity = errors.New("managed Agent upgrade capacity exhausted")
const MaxActiveTasks = 3

// Admission audit is immutable and stored atomically with its task.
type admissionRow struct {
 TaskID string `gorm:"primaryKey;size:128;not null"`
 OperatorID uint64 `gorm:"not null"`
 AdmittedAt time.Time `gorm:"not null"`
 Snapshot []byte `gorm:"not null"`
}
func(admissionRow) TableName()string{return "managed_agent_upgrade_admissions"}

// A nullable unique active-server ID reserves a server until an explicit
// terminal transition. Expiration alone never releases the reservation.
type taskRow struct {
 ID string `gorm:"primaryKey;size:128;not null"`
 ServerID uint64 `gorm:"not null;index"`
 ActiveServerID *uint64 `gorm:"uniqueIndex"`
 Revision int64 `gorm:"not null"`
 Snapshot []byte `gorm:"not null"`
}

func (taskRow) TableName() string { return "managed_agent_upgrade_tasks" }

type Record struct {
 Task core.Transaction
 Revision int64
}

type Store struct { db *gorm.DB }

// Open migrates only the new task table. The caller supplies the Dashboard DB.
// This is not called at application startup until the feature is integrated.
func Open(ctx context.Context, db *gorm.DB) (*Store,error) {
 if db==nil { return nil,errors.New("missing upgrade database") }
 if db.Dialector.Name()!="sqlite" { return nil,errors.New("unsupported upgrade database") }
 if err:=db.WithContext(ctx).AutoMigrate(&taskRow{},&admissionRow{},&deliveryRow{},&executionRow{},&abortedReservationRow{});err!=nil { return nil,err }
 return &Store{db:db},nil
}

func active(t core.Transaction) *uint64 {
 switch t.State {
 case core.StateSucceeded,core.StateRolledBack,core.StateFailed:
  return nil
 default:
  id:=t.ServerID
  return &id
 }
}

func validID(id string) bool {
 if len(id)==0 || len(id)>128 { return false }
 for _,c:=range id {
  if !(c>='a' && c<='z' || c>='A' && c<='Z' || c>='0' && c<='9' || c=='-' || c=='_') { return false }
 }
 return true
}

func decode(r taskRow) (Record,error) {
 t,err:=core.DecodeSnapshot(r.Snapshot)
 if err!=nil { return Record{},err }
 a:=active(t)
 if !validID(t.ID) || t.ServerID>math.MaxInt64 || r.ID!=t.ID || r.ServerID!=t.ServerID || r.Revision<1 ||
  (a==nil)!=(r.ActiveServerID==nil) || (a!=nil && *a!=*r.ActiveServerID) {
  return Record{},errors.New("inconsistent upgrade task record")
 }
 return Record{Task:t,Revision:r.Revision},nil
}

// Create reserves a server atomically using the unique index. The caller must
// have independently authorized the operator, target and verified package.
func (s *Store) Create(ctx context.Context,t core.Transaction,now time.Time)(Record,error) {
 return s.create(ctx,t,now,0)
}

// create reserves global capacity and, for scheduler admission, records the
// authenticated operator in the same SQLite transaction. Concurrent writers
// may receive SQLITE_BUSY; never treat a busy error as admission success.
func(s *Store) create(ctx context.Context,t core.Transaction,now time.Time,operator uint64)(Record,error){
 if operator>math.MaxInt64 || !validID(t.ID) || t.ServerID>math.MaxInt64 || t.State!=core.StatePending || now.IsZero() || !now.Before(t.ExpiresAt) {
  return Record{},errors.New("invalid initial upgrade task")
 }
 b,err:=core.EncodeSnapshot(t)
 if err!=nil { return Record{},err }
 r:=taskRow{ID:t.ID,ServerID:t.ServerID,ActiveServerID:active(t),Revision:1,Snapshot:b}
 if err:=s.db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  var count int64
  if err:=tx.Model(&taskRow{}).Where("active_server_id IS NOT NULL").Count(&count).Error;err!=nil{return err}
  if count>=MaxActiveTasks{return ErrCapacity}
  if err:=tx.Create(&r).Error;err!=nil{return err}
  if operator!=0{return tx.Create(&admissionRow{TaskID:t.ID,OperatorID:operator,AdmittedAt:now.UTC(),Snapshot:b}).Error}
  return nil
 });err!=nil{return Record{},err}
 return decode(r)
}

func (s *Store) Get(ctx context.Context,id string)(Record,error) {
 if !validID(id) { return Record{},errors.New("invalid upgrade task ID") }
 var r taskRow
 if err:=s.db.WithContext(ctx).First(&r,"id = ?",id).Error;err!=nil { return Record{},err }
 return decode(r)
}

// Advance is a compare-and-swap on the persisted snapshot. It does not treat
// a caller-supplied receipt as proof of authorization or Agent health. Those
// checks must precede invocation in the authenticated scheduler.
func (s *Store) Advance(ctx context.Context,expected Record,next core.State,now time.Time)(Record,error) {
 current,err:=s.Get(ctx,expected.Task.ID)
 if err!=nil { return Record{},err }
 if current.Revision!=expected.Revision || !current.Task.SameIdentity(expected.Task) ||
  current.Task.State!=expected.Task.State || !current.Task.ExpiresAt.Equal(expected.Task.ExpiresAt) {
  return Record{},ErrConflict
 }
 if current.Revision==math.MaxInt64 { return Record{},errors.New("upgrade revision exhausted") }
 t,err:=current.Task.Transition(next,now)
 if err!=nil { return Record{},err }
 old,err:=core.EncodeSnapshot(current.Task)
 if err!=nil { return Record{},err }
 b,err:=core.EncodeSnapshot(t)
 if err!=nil { return Record{},err }
 // Snapshot predicate additionally rejects unexpected out-of-band mutation.
 // Re-encoding a noncanonical snapshot fails closed.
 res:=s.db.WithContext(ctx).Model(&taskRow{}).
  Where("id = ? AND revision = ? AND snapshot = ?",t.ID,current.Revision,old).
  Updates(map[string]interface{}{"snapshot":b,"revision":current.Revision+1,"active_server_id":active(t)})
 if res.Error!=nil { return Record{},res.Error }
 if res.RowsAffected!=1 { return Record{},ErrConflict }
 return Record{Task:t,Revision:current.Revision+1},nil
}

// ActivePage is a bounded recovery scan; callers must not equate a scan with
// ownership of a task. Recovery actions must still use revision CAS.
func (s *Store) ActivePage(ctx context.Context,after string,limit int)([]Record,error) {
 if limit<1 || limit>200 || (after!="" && !validID(after)) { return nil,errors.New("invalid recovery page") }
 var rows []taskRow
 err:=s.db.WithContext(ctx).Where("active_server_id IS NOT NULL AND id > ?",after).
  Order("id ASC").Limit(limit).Find(&rows).Error
 if err!=nil { return nil,err }
 result:=make([]Record,0,len(rows))
 for _,r:=range rows {
  v,err:=decode(r)
  if err!=nil { return nil,fmt.Errorf("invalid persisted upgrade task: %w",err) }
  result=append(result,v)
 }
 return result,nil
}

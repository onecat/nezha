package agentupgrade

import (
 "context"
 "crypto/rand"
 "encoding/hex"
 "errors"
 "math"
 "os"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
)

// Eligibility resolves a trusted, current operator/target authorization and
// Agent capability from server-side state. It must authenticate the caller
// outside this module and never copy flags or the high-water mark from an
// untrusted request. No production implementation is wired yet.
type Eligibility interface {
 Policy(context.Context,uint64,uint64)(core.AuthorizationPolicy,uint64,error)
}

type SchedulerConfig struct {
 Enabled bool
 // Explicit local operator approval after platform qualification.
 LinuxAMD64 bool
 LinuxARM64 bool
}

type Scheduler struct{
 store *Store
 packages *Packages
 eligibility Eligibility
 config SchedulerConfig
}

func NewScheduler(store *Store,packages *Packages,eligibility Eligibility,cfg SchedulerConfig)(*Scheduler,error){
 if store==nil||packages==nil||eligibility==nil{return nil,errors.New("missing scheduler dependencies")}
 return &Scheduler{store:store,packages:packages,eligibility:eligibility,config:cfg},nil
}

func(s *Scheduler) authorize(ctx context.Context,operator uint64,t core.Transaction,p core.VerifiedPackage)error{
 if !s.config.Enabled||operator==0||operator>math.MaxInt64||t.ServerID==0||t.ServerID>math.MaxInt64{
  return errors.New("managed upgrades disabled or invalid identity")
 }
 policy,highest,err:=s.eligibility.Policy(ctx,operator,t.ServerID)
 if err!=nil{return err}
 // Package format remains generic; actual dispatch is Linux-only.
 // Even an affirmative capability claim cannot bypass local qualification.
 if policy.AgentOS!="linux"||!((policy.AgentArch=="amd64"&&s.config.LinuxAMD64)||(policy.AgentArch=="arm64"&&s.config.LinuxARM64))||highest==0{
  return errors.New("unqualified upgrade platform or missing trusted sequence")
 }
 policy.Now=time.Now()
 return policy.AuthorizeUpgrade(t,p,highest)
}

// Admit receives the operator ID only from a trusted authenticated handler.
// It reopens the signed package and persists a fresh task plus admission audit.
// A 15-minute fixed expiry limits stale approvals; this is not a batch queue.
func(s *Scheduler) Admit(ctx context.Context,operator,server uint64,digest string)(Record,error){
 if !s.config.Enabled||operator==0||server==0{return Record{},errors.New("managed upgrades disabled or invalid identity")}
 f,p,err:=s.packages.Open(digest)
 if err!=nil{return Record{},err}
 defer f.Close()
 var nonce [16]byte
 if _,err:=rand.Read(nonce[:]);err!=nil{return Record{},err}
 now:=time.Now()
 t:=core.Transaction{ID:hex.EncodeToString(nonce[:]),ServerID:server,PackageSHA256:p.PackageSHA256,Version:p.Manifest.Version,ReleaseSequence:p.Manifest.ReleaseSequence,State:core.StatePending,ExpiresAt:now.Add(15*time.Minute)}
 if err:=s.authorize(ctx,operator,t,p);err!=nil{return Record{},err}
 return s.store.create(ctx,t,time.Now(),operator)
}

// Transfer owns the same read-only verified descriptor used by Sender.
// A future authenticated transport must bind every message to its session;
// possessing this object is not a network authentication mechanism.
// The single owner must close it, and handle a failed/expired transfer by
// a valid state transition. No automatic resume or acknowledgement here.
type Transfer struct{
 Record Record
 Size int64
 file *os.File
 sender *core.Sender
}

func(t *Transfer) ChunkAt(ctx context.Context,offset int64)(core.Chunk,bool,error){
 if t.file==nil{return core.Chunk{},false,errors.New("transfer closed")}
 if err:=ctx.Err();err!=nil{return core.Chunk{},false,err}
 if !time.Now().Before(t.Record.Task.ExpiresAt){return core.Chunk{},false,errors.New("transfer expired")}
 return t.sender.ChunkAt(offset)
}
func(t *Transfer) Close()error{
 if t.file==nil{return nil}
 err:=t.file.Close();t.file=nil;return err
}

// BeginTransfer only accepts a server ID from an authenticated Agent session.
// It reloads the original operator and immutable approval, rechecks current
// eligibility and the signed package, then claims the pending task by CAS.
// Two callers cannot both acquire the same pending task. No stream is opened
// by this method, and no install/success state is inferred.
func(s *Scheduler) BeginTransfer(ctx context.Context,authenticatedServer uint64,id string)(*Transfer,error){
 if !s.config.Enabled||authenticatedServer==0{return nil,errors.New("managed upgrades disabled or missing Agent identity")}
 r,err:=s.store.Get(ctx,id)
 if err!=nil{return nil,err}
 if r.Task.State!=core.StatePending||r.Task.ServerID!=authenticatedServer{return nil,errors.New("task not available to Agent")}
 var audit admissionRow
 if err:=s.store.db.WithContext(ctx).First(&audit,"task_id = ?",id).Error;err!=nil{return nil,err}
 original,err:=core.DecodeSnapshot(audit.Snapshot)
 if err!=nil||audit.TaskID!=id||audit.OperatorID==0||audit.AdmittedAt.IsZero()||
  original.State!=core.StatePending||!original.SameIdentity(r.Task)||!original.ExpiresAt.Equal(r.Task.ExpiresAt){
  return nil,errors.New("invalid admission audit")
 }
 f,p,err:=s.packages.Open(r.Task.PackageSHA256)
 if err!=nil{return nil,err}
 keep:=false
 defer func(){if !keep{f.Close()}}()
 if err:=s.authorize(ctx,audit.OperatorID,r.Task,p);err!=nil{return nil,err}
 st,err:=f.Stat()
 if err!=nil{return nil,err}
 next,err:=r.Task.Transition(core.StateTransferring,time.Now())
 if err!=nil{return nil,err}
 sender,err:=core.NewSender(next,f,st.Size())
 if err!=nil{return nil,err}
 claimed,err:=s.store.Advance(ctx,r,core.StateTransferring,time.Now())
 if err!=nil{return nil,err}
 keep=true
 return &Transfer{Record:claimed,Size:st.Size(),file:f,sender:sender},nil
}

// Package upgradecontrol bridges local Dashboard administration and the
// managed upgrade scheduler. Configuration is local-only and immutable.
package upgradecontrol

import (
 "context"
 "crypto/ed25519"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "os"
 "path/filepath"
 "time"

 "github.com/naiba/nezha/model"
 core "github.com/naiba/nezha/pkg/agentupgrade"
 upgrade "github.com/naiba/nezha/service/agentupgrade"
 "github.com/naiba/nezha/service/singleton"
)

type target struct{
 ID uint64 `json:"id"`
 SecretSHA256 string `json:"secret_sha256"`
 Arch string `json:"arch"`
 InitialSequence uint64 `json:"initial_sequence"`
}
type settings struct{
 Enabled bool `json:"enabled"`
 AllowExecution bool `json:"allow_execution"`
 PackageDir string `json:"package_dir"`
 LinuxAMD64 bool `json:"linux_amd64"`
 LinuxARM64 bool `json:"linux_arm64"`
 Keys map[string]string `json:"keys"`
 Targets []target `json:"targets"`
}
type Control struct{
 Scheduler *upgrade.Scheduler
 Execution bool
 store *upgrade.Store
 targets map[uint64]target
}
// Current is initialized once before starting web and RPC listeners.
var Current *Control

func Load(ctx context.Context,path string)(*Control,error){
 if path==""{return nil,nil}
 if !filepath.IsAbs(path){return nil,errors.New("upgrade config path must be absolute")}
 before,err:=os.Lstat(path)
 if err!=nil||!before.Mode().IsRegular()||before.Mode().Perm()&0077!=0{return nil,errors.New("upgrade config must be private")}
 f,err:=os.Open(path)
 if err!=nil{return nil,err}
 defer f.Close()
 st,err:=f.Stat()
 if err!=nil||!os.SameFile(before,st)||st.Size()>65536{return nil,errors.New("invalid upgrade config")}
 dec:=json.NewDecoder(io.LimitReader(f,65537))
 dec.DisallowUnknownFields()
 var cfg settings
 if err:=dec.Decode(&cfg);err!=nil{return nil,errors.New("invalid upgrade config")}
 if err:=dec.Decode(new(any));err!=io.EOF{return nil,errors.New("trailing upgrade config")}
 if !cfg.Enabled{return nil,nil}
 if len(cfg.Targets)==0||len(cfg.Targets)>1024||len(cfg.Keys)==0||len(cfg.Keys)>16{return nil,errors.New("missing or excessive upgrade provisioning")}
 keys:=map[string]ed25519.PublicKey{}
 for id,value:=range cfg.Keys{
  b,err:=hex.DecodeString(value)
  if id==""||err!=nil||len(b)!=ed25519.PublicKeySize{return nil,errors.New("invalid upgrade public key")}
  keys[id]=ed25519.PublicKey(b)
 }
 c:=&Control{Execution:cfg.AllowExecution,targets:map[uint64]target{}}
 for _,v:=range cfg.Targets{
  b,err:=hex.DecodeString(v.SecretSHA256)
  if v.ID==0||v.InitialSequence==0||err!=nil||len(b)!=32||hex.EncodeToString(b)!=v.SecretSHA256||
   !((v.Arch=="amd64"&&cfg.LinuxAMD64)||(v.Arch=="arm64"&&cfg.LinuxARM64)){return nil,errors.New("invalid provisioned target")}
  if _,exists:=c.targets[v.ID];exists{return nil,errors.New("duplicate target")}
  c.targets[v.ID]=v
 }
 p,err:=upgrade.OpenPackages(cfg.PackageDir,keys)
 if err!=nil{return nil,err}
 c.store,err=upgrade.Open(ctx,singleton.DB)
 if err!=nil{return nil,err}
 c.Scheduler,err=upgrade.NewScheduler(c.store,p,c,upgrade.SchedulerConfig{Enabled:true,LinuxAMD64:cfg.LinuxAMD64,LinuxARM64:cfg.LinuxARM64})
 if err!=nil{return nil,err}
 return c,nil
}

// Operator requires the currently configured local superadmin, with a
// live database session. No generic member or API token grants authority.
func Operator(ctx context.Context,id uint64)bool{
 if id==0||singleton.Conf==nil||singleton.DB==nil||singleton.Conf.Oauth2.Type!=model.ConfigTypeLocal{return false}
 var u model.User
 if err:=singleton.DB.WithContext(ctx).First(&u,"id = ?",id).Error;err!=nil{return false}
 return u.SuperAdmin&&u.Login=="local:"+singleton.Conf.LocalAuth.Username&&u.Token!=""&&u.TokenExpired.After(time.Now())
}

func(c *Control) targetAllowed(id uint64)(target,*model.Server,bool){
 v,ok:=c.targets[id]
 s:=singleton.SnapshotServer(id)
 if !ok||s==nil||s.Secret==""{return v,s,false}
 sum:=sha256.Sum256([]byte(s.Secret))
 return v,s,hex.EncodeToString(sum[:])==v.SecretSHA256
}
func(c *Control) AgentAllowed(id uint64)bool{
 _,_,ok:=c.targetAllowed(id)
 return ok
}
func(c *Control) Policy(ctx context.Context,operator,id uint64)(core.AuthorizationPolicy,uint64,error){
 var p core.AuthorizationPolicy
 v,s,ok:=c.targetAllowed(id)
 if !ok||!Operator(ctx,operator)||s.Host==nil||s.Host.Arch!=v.Arch||s.Host.Version==""||
  s.LastActive.IsZero()||time.Since(s.LastActive)>90*time.Second{return p,0,errors.New("operator or target not eligible")}
 // Capability is explicitly provisioned by the local operator, never
 // inferred from an arbitrary self-reported version. The Agent also
 // enforces native Linux platform and local signed high-water state.
 high,err:=c.store.HighWater(ctx,id,v.InitialSequence)
 if err!=nil{return p,0,err}
 return core.AuthorizationPolicy{Enabled:true,OperatorCanUpgrade:true,ServerAllowsUpgrade:true,AgentSupportsUpgrade:true,AgentVersion:s.Host.Version,AgentOS:"linux",AgentArch:v.Arch},high,nil
}

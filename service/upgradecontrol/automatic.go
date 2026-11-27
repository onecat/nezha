package upgradecontrol

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "net/netip"
 "os"
 "path/filepath"

 upgrade "github.com/naiba/nezha/service/agentupgrade"
 "github.com/naiba/nezha/service/singleton"
 "gorm.io/gorm/clause"
)

type enrolledTarget struct{
 ServerID uint64 `gorm:"primaryKey;autoIncrement:false"`
 SecretSHA256 string `gorm:"not null"`
}
type automaticControl struct{
 key ed25519.PrivateKey
 dir string
 gate chan struct{}
}
func(c *Control)Automatic()bool{return c!=nil&&c.automatic!=nil}

// No automatic change to legacy wildcard listeners. The URL feature is only
// activated for an explicit same-host TLS reverse-proxy deployment.
func LoadAutomatic(ctx context.Context,dbPath string)(*Control,error){
 conf:=singleton.Conf
 if !conf.TLS{return nil,nil}
 for _,h:=range []string{conf.HTTPBindHost,conf.GRPCBindHost}{
  ip,err:=netip.ParseAddr(h);if err!=nil||!ip.IsLoopback(){return nil,nil}
 }
 root,err:=filepath.Abs(dbPath+".url-upgrade");if err!=nil{return nil,err}
 if err:=privateDir(root);err!=nil{return nil,err}
 packages:=filepath.Join(root,"packages")
 if err:=privateDir(packages);err!=nil{return nil,err}
 key,err:=localSigningKey(root);if err!=nil{return nil,err}
 if err:=singleton.DB.WithContext(ctx).AutoMigrate(&enrolledTarget{});err!=nil{return nil,err}
 c:=&Control{Execution:true,automatic:&automaticControl{key:key,dir:root,gate:make(chan struct{},1)}}
 store,err:=upgrade.Open(ctx,singleton.DB);if err!=nil{return nil,err};c.store=store
 p,err:=upgrade.OpenPackages(packages,map[string]ed25519.PublicKey{"dashboard-local":key.Public().(ed25519.PublicKey)});if err!=nil{return nil,err}
 c.Scheduler,err=upgrade.NewScheduler(store,p,c,upgrade.SchedulerConfig{Enabled:true,LinuxAMD64:true})
 if err!=nil{return nil,err}
 return c,nil
}
func privateDir(p string)error{
 if err:=os.Mkdir(p,0700);err!=nil&&!errors.Is(err,os.ErrExist){return err}
 st,err:=os.Lstat(p);if err!=nil||!st.IsDir()||st.Mode().Perm()&0077!=0{return errors.New("private URL upgrade directory required")}
 return nil
}
func localSigningKey(root string)(ed25519.PrivateKey,error){
 path:=filepath.Join(root,"signing.seed")
 st,err:=os.Lstat(path)
 if errors.Is(err,os.ErrNotExist){
  seed:=make([]byte,ed25519.SeedSize);if _,err:=rand.Read(seed);err!=nil{return nil,err}
  f,err:=os.CreateTemp(root,".key-");if err!=nil{return nil,err};defer os.Remove(f.Name());defer f.Close()
  if _,err=f.Write(seed);err!=nil{return nil,err};if err=f.Sync();err!=nil{return nil,err}
  if err=os.Link(f.Name(),path);err!=nil&&!errors.Is(err,os.ErrExist){return nil,err}
  d,err:=os.Open(root);if err!=nil{return nil,err};err=d.Sync();d.Close();if err!=nil{return nil,err}
  st,err=os.Lstat(path)
 }
 if err!=nil||!st.Mode().IsRegular()||st.Mode().Perm()&0077!=0||st.Size()!=ed25519.SeedSize{return nil,errors.New("invalid private signing seed")}
 file,err:=os.Open(path);if err!=nil{return nil,err};defer file.Close()
 actual,err:=file.Stat();if err!=nil||!os.SameFile(st,actual){return nil,errors.New("signing seed changed")}
 seed:=make([]byte,ed25519.SeedSize)
 if _,err:=file.ReadAt(seed,0);err!=nil{return nil,err}
 return ed25519.NewKeyFromSeed(seed),nil
}
func(c *Control)Enroll(ctx context.Context,id uint64)(ed25519.PublicKey,error){
 if !c.Automatic()||id==0{return nil,errors.New("automatic enrollment disabled")}
 s:=singleton.SnapshotServer(id);if s==nil||s.Secret==""{return nil,errors.New("unknown Agent")}
 sum:=sha256.Sum256([]byte(s.Secret));e:=enrolledTarget{ServerID:id,SecretSHA256:hex.EncodeToString(sum[:])}
 // A changed/reused secret cannot silently inherit prior enrollment.
 if err:=singleton.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing:true}).Create(&e).Error;err!=nil{return nil,err}
 if !c.AgentAllowed(id){return nil,errors.New("enrollment identity changed")}
 return c.automatic.key.Public().(ed25519.PublicKey),nil
}

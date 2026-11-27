package upgradecontrol

import(
 "context"
 "crypto/sha256"
 "debug/buildinfo"
 "debug/elf"
 "encoding/hex"
 "errors"
 "fmt"
 "io"
 "os"
 "strings"

 core "github.com/naiba/nezha/pkg/agentupgrade"
 upgrade "github.com/naiba/nezha/service/agentupgrade"
 "github.com/naiba/nezha/service/singleton"
 "gorm.io/gorm"
 "gorm.io/gorm/clause"
)

// ReleaseSelection is one operator-approved runtime setting. Its revision
// is bound to the confirmation request, not to the Dashboard build.
type ReleaseSelection struct{
 ID uint64 `json:"-" gorm:"primaryKey;autoIncrement:false"`
 URL string `json:"url"`
 Version string `json:"version"`
 SHA256 string `json:"sha256"`
 Revision uint64 `json:"revision"`
}
func(r ReleaseSelection) Valid()bool{
 v,err:=parseRelease(r.URL)
 b,e:=hex.DecodeString(r.SHA256)
 return err==nil&&strings.HasSuffix(r.URL,"/nezha-agent")&&v==r.Version&&e==nil&&len(b)==32&&hex.EncodeToString(b)==r.SHA256&&r.Revision>0
}
func readSelection(ctx context.Context,db *gorm.DB)(ReleaseSelection,error){
 var r ReleaseSelection
 err:=db.WithContext(ctx).First(&r,"id = ?",1).Error
 if errors.Is(err,gorm.ErrRecordNotFound){return ReleaseSelection{},nil}
 if err!=nil{return ReleaseSelection{},err}
 if !r.Valid(){return ReleaseSelection{},errors.New("invalid stored Agent release selection")}
 return r,nil
}
func(c *Control) Selection(ctx context.Context)(ReleaseSelection,error){
 if !c.Automatic()||!c.Execution{return ReleaseSelection{},errors.New("automatic upgrades unavailable")}
 return readSelection(ctx,singleton.DB)
}
func persistSelection(ctx context.Context,db *gorm.DB,next ReleaseSelection)(ReleaseSelection,error){
 // Serialized by the automatic gate in production. Transaction protects
 // revision and the previous good selection from partial writes.
 err:=db.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  prior,err:=readSelection(ctx,tx);if err!=nil{return err}
  if prior.Valid()&&prior.URL==next.URL&&prior.SHA256!=next.SHA256{return errors.New("same release URL changed checksum; publish a new version")}
  if prior.Revision>=uint64(1<<63-1){return errors.New("selection revision exhausted")}
  next.ID=1;next.Revision=prior.Revision+1
  if !next.Valid(){return errors.New("invalid release selection")}
  return tx.Clauses(clause.OnConflict{UpdateAll:true}).Create(&next).Error
 })
 if err!=nil{return ReleaseSelection{},err}
 return next,nil
}
// Fetch from the exact versioned independent release. Never execute
// downloaded bytes on Dashboard. A pin must match both published sums and
// content; changing release metadata after confirmation is not authority.
func(c *Control) verifiedRelease(ctx context.Context,raw,pin string)(ReleaseSelection,*os.File,int64,error){
 version,err:=parseRelease(raw)
 if err!=nil||!strings.HasSuffix(raw,"/nezha-agent"){return ReleaseSelection{},nil,0,errors.New("expected independent versioned single-binary Agent URL")}
 client:=releaseClient();defer client.CloseIdleConnections()
 var sums strings.Builder
 if _,err:=fetch(ctx,client,releasePrefix+version+"/SHA256SUMS",64<<10,&sums);err!=nil{return ReleaseSelection{},nil,0,err}
 want,err:=checksum(sums.String(),"nezha-agent");if err!=nil{return ReleaseSelection{},nil,0,err}
 if pin!=""&&pin!=want{return ReleaseSelection{},nil,0,errors.New("release checksum changed since approval")}
 binary,err:=os.CreateTemp(c.automatic.dir,".binary-");if err!=nil{return ReleaseSelection{},nil,0,err}
 keep:=false
 defer func(){if !keep{binary.Close();os.Remove(binary.Name())}}()
 digest:=sha256.New()
 size,err:=fetch(ctx,client,raw,core.MaxPackageSize-65536,io.MultiWriter(binary,digest));if err!=nil{return ReleaseSelection{},nil,0,err}
 if hex.EncodeToString(digest.Sum(nil))!=want{return ReleaseSelection{},nil,0,errors.New("release checksum mismatch")}
 if err:=validateStaticAgent(binary);err!=nil{return ReleaseSelection{},nil,0,err}
 keep=true
 return ReleaseSelection{URL:raw,Version:version,SHA256:want},binary,size,nil
}
func validateStaticAgent(binary io.ReaderAt)error{
 ef,err:=elf.NewFile(binary);if err!=nil{return errors.New("not an ELF binary")};defer ef.Close()
 if ef.Class!=elf.ELFCLASS64||ef.Machine!=elf.EM_X86_64||ef.Data!=elf.ELFDATA2LSB{return errors.New("wrong executable architecture")}
 for _,p:=range ef.Progs{if p.Type==elf.PT_INTERP{return errors.New("Agent must be static: ELF interpreter present")}}
 libs,err:=ef.ImportedLibraries();if err!=nil||len(libs)!=0{return errors.New("Agent must be static: shared-library dependencies")}
 info,err:=buildinfo.Read(binary);if err!=nil||info.Path!="github.com/onecat/nezha-agent/cmd/agent"{return errors.New("not the independent Agent binary")}
 settings:=map[string]string{}
 for _,s:=range info.Settings{settings[s.Key]=s.Value}
 if settings["CGO_ENABLED"]!="0"||settings["GOOS"]!="linux"||settings["GOARCH"]!="amd64"||settings["GOAMD64"]!="v1"{return errors.New("Agent requires pure-Go Linux amd64 v1 build")}
 return nil
}
func(c *Control) SaveSelection(ctx context.Context,operator uint64,raw string)(ReleaseSelection,error){
 if !c.Automatic()||!c.Execution||!Operator(ctx,operator){return ReleaseSelection{},errors.New("Agent selection disabled")}
 select{case c.automatic.gate<-struct{}{}:defer func(){<-c.automatic.gate}();default:return ReleaseSelection{},errors.New("another release operation is running")}
 r,b,_,err:=c.verifiedRelease(ctx,raw,"");if err!=nil{return ReleaseSelection{},err}
 defer b.Close();defer os.Remove(b.Name())
 if !Operator(ctx,operator){return ReleaseSelection{},errors.New("operator session expired")}
 return persistSelection(ctx,singleton.DB,r)
}

type PreviewTarget struct{
 ID uint64 `json:"id"`
 Name string `json:"name"`
 Current string `json:"current"`
 Eligible bool `json:"eligible"`
 Reason string `json:"reason"`
}
type UpgradePreview struct{
 Release ReleaseSelection `json:"release"`
 Targets []PreviewTarget `json:"targets"`
}
func forwardEligible(version,current string,high uint64)bool{
 if high==^uint64(0){return false}
 m:=core.Manifest{FormatVersion:1,Component:"nezha-agent",Version:version,OS:"linux",Arch:"amd64",MinAgentVersion:"v0.30.00",ProtocolVersion:core.ProtocolVersion,Filename:"nezha-agent",Size:1,SHA256:strings.Repeat("0",64),SigningKeyID:"dashboard-local",ReleaseSequence:high+1}
 return m.CheckUpgrade("linux","amd64",current,high)==nil
}
func(c *Control) Preview(ctx context.Context,operator uint64,servers []uint64)(UpgradePreview,error){
 var out UpgradePreview
 if !c.Automatic()||!c.Execution||!Operator(ctx,operator){return out,errors.New("upgrade unavailable")}
 if len(servers)==0||len(servers)>upgrade.MaxActiveTasks{return out,errors.New("select one to three servers")}
 r,err:=c.Selection(ctx);if err!=nil||!r.Valid(){return out,errors.New("configure Agent installation release first")}
 out.Release=r
 seen:=map[uint64]bool{}
 for _,id:=range servers{
  if id==0||seen[id]{return UpgradePreview{},errors.New("duplicate or invalid server")}
  seen[id]=true
  row:=PreviewTarget{ID:id,Name:fmt.Sprintf("#%d",id),Reason:"未登记、离线或平台不支持"}
  if s:=singleton.SnapshotServer(id);s!=nil{row.Name=s.Name;if s.Host!=nil{row.Current=s.Host.Version}}
  p,h,e:=c.Policy(ctx,operator,id)
  if e==nil&&p.AgentArch=="amd64"{
   row.Current=p.AgentVersion
   if forwardEligible(r.Version,p.AgentVersion,h){row.Eligible=true;row.Reason=""}else{row.Reason="版本相同、较新或不支持向前升级"}
  }
  out.Targets=append(out.Targets,row)
 }
 return out,nil
}

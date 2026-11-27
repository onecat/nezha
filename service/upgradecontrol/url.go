package upgradecontrol

import(
 "context"
 "crypto/sha256"
 "debug/elf"
 "debug/buildinfo"
 "encoding/hex"
 "errors"
 "io"
 "net"
 "net/http"
 "net/url"
 "os"
 "regexp"
 "strings"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
 upgrade "github.com/naiba/nezha/service/agentupgrade"
)

const releasePrefix="https://github.com/onecat/nezha-agent/releases/download/"
const binaryName="nezha-agent-linux-amd64"
var releaseVersion=regexp.MustCompile(`^v[0-9]{1,9}\.[0-9]{1,9}\.[0-9]{1,9}$`)
func parseRelease(raw string)(string,error){
 if !strings.HasPrefix(raw,releasePrefix){return "",errors.New("only the configured independent Agent repository is accepted")}
 parts:=strings.Split(strings.TrimPrefix(raw,releasePrefix),"/")
 if len(parts)!=2||!releaseVersion.MatchString(parts[0])||parts[1]!=binaryName{return "",errors.New("expected a versioned raw Linux amd64 Agent binary URL")}
 return parts[0],nil
}
func releaseHost(u *url.URL)bool{
 if u==nil||u.Scheme!="https"||u.User!=nil||u.Port()!=""||u.Fragment!=""{return false}
 switch u.Hostname(){case "github.com","release-assets.githubusercontent.com","objects.githubusercontent.com":return true}
 return false
}
func releaseClient()*http.Client{
 tr:=&http.Transport{Proxy:nil,DialContext:(&net.Dialer{Timeout:10*time.Second}).DialContext,TLSHandshakeTimeout:10*time.Second,ResponseHeaderTimeout:20*time.Second,DisableCompression:true}
 return &http.Client{Transport:tr,Timeout:2*time.Minute,CheckRedirect:func(req *http.Request,via []*http.Request)error{
  if len(via)>5||!releaseHost(req.URL){return errors.New("untrusted release redirect")}
  return nil
 }}
}
func fetch(ctx context.Context,client *http.Client,raw string,limit int64,dst io.Writer)(int64,error){
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,raw,nil);if err!=nil||!releaseHost(req.URL){return 0,errors.New("invalid release URL")}
 resp,err:=client.Do(req);if err!=nil{return 0,errors.New("release download failed")};defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK||resp.ContentLength>limit{return 0,errors.New("release unavailable or oversized")}
 n,err:=io.Copy(dst,io.LimitReader(resp.Body,limit+1));if err!=nil||n==0||n>limit{return 0,errors.New("invalid release size")}
 return n,nil
}
func checksum(data string)(string,error){
 var found string
 for _,line:=range strings.Split(data,"\n"){
  fields:=strings.Fields(line)
  if len(fields)!=2||strings.TrimPrefix(fields[1],"*")!=binaryName{continue}
  if found!=""{return "",errors.New("duplicate release checksum")}
  b,err:=hex.DecodeString(fields[0]);if err!=nil||len(b)!=32||hex.EncodeToString(b)!=fields[0]{return "",errors.New("invalid release checksum")}
  found=fields[0]
 }
 if found==""{return "",errors.New("binary checksum missing")}
 return found,nil
}
func(c *Control)UpgradeURL(ctx context.Context,operator uint64,servers []uint64,raw string)([]upgrade.Record,error){
 if !c.Automatic()||!c.Execution||!Operator(ctx,operator){return nil,errors.New("URL upgrade disabled")}
 version,err:=parseRelease(raw);if err!=nil{return nil,err}
 if len(servers)==0||len(servers)>upgrade.MaxActiveTasks{return nil,errors.New("select one to three enrolled Agents")}
 select{case c.automatic.gate<-struct{}{}:defer func(){<-c.automatic.gate}();default:return nil,errors.New("another URL upgrade is being prepared")}
 seen:=map[uint64]bool{};high:=uint64(1)
 for _,id:=range servers{
  if seen[id]{return nil,errors.New("duplicate Agent")};seen[id]=true
  p,h,err:=c.Policy(ctx,operator,id);if err!=nil||p.AgentArch!="amd64"{return nil,errors.New("Agent not enrolled or offline")}
  if h>high{high=h}
  // Validate version before any external fetch; same or older versions fail.
  m:=core.Manifest{FormatVersion:1,Component:"nezha-agent",Version:version,OS:"linux",Arch:"amd64",MinAgentVersion:"v0.30.00",ProtocolVersion:core.ProtocolVersion,Filename:"nezha-agent",Size:1,SHA256:strings.Repeat("0",64),SigningKeyID:"dashboard-local",ReleaseSequence:h+1}
  if h==^uint64(0)||m.CheckUpgrade("linux","amd64",p.AgentVersion,h)!=nil{return nil,errors.New("target is not an eligible forward upgrade")}
 }
 if high==^uint64(0){return nil,errors.New("sequence exhausted")}
 client:=releaseClient();defer client.CloseIdleConnections()
 var sums strings.Builder
 if _,err:=fetch(ctx,client,releasePrefix+version+"/SHA256SUMS",64<<10,&sums);err!=nil{return nil,err}
 want,err:=checksum(sums.String());if err!=nil{return nil,err}
 binary,err:=os.CreateTemp(c.automatic.dir,".binary-");if err!=nil{return nil,err};defer os.Remove(binary.Name());defer binary.Close()
 digest:=sha256.New()
 size,err:=fetch(ctx,client,raw,core.MaxPackageSize-65536,io.MultiWriter(binary,digest));if err!=nil{return nil,err}
 if hex.EncodeToString(digest.Sum(nil))!=want{return nil,errors.New("release checksum mismatch")}
 ef,err:=elf.NewFile(binary);if err!=nil{return nil,errors.New("not an ELF binary")};defer ef.Close()
 if ef.Class!=elf.ELFCLASS64||ef.Machine!=elf.EM_X86_64||ef.Data!=elf.ELFDATA2LSB{return nil,errors.New("wrong executable architecture")}
 info,err:=buildinfo.Read(binary);if err!=nil||info.Path!="github.com/onecat/nezha-agent/cmd/agent"{return nil,errors.New("not the independent Agent binary")}
 pack,err:=os.CreateTemp(c.automatic.dir,".package-");if err!=nil{return nil,err};defer os.Remove(pack.Name());defer pack.Close()
 manifest:=core.Manifest{Version:version,OS:"linux",Arch:"amd64",MinAgentVersion:"v0.30.00",SigningKeyID:"dashboard-local",ReleaseSequence:high+1}
 if _,err:=core.BuildPackage(pack,binary,size,manifest,c.automatic.key);err!=nil{return nil,err}
 if _,err:=pack.Seek(0,io.SeekStart);err!=nil{return nil,err}
 verified,err:=c.Scheduler.Import(ctx,pack);if err!=nil{return nil,err}
 if err:=c.Scheduler.ExpireUnstarted(ctx);err!=nil{return nil,err}
 var tasks []upgrade.Record
 // User explicitly approves this bounded batch; no manually copied canary ID.
 // Every target still passes the scheduler's independent authorization.
 for _,id:=range servers{
  r,err:=c.Scheduler.Admit(ctx,operator,id,verified.PackageSHA256);if err!=nil{return tasks,err}
  tasks=append(tasks,r)
 }
 return tasks,nil
}

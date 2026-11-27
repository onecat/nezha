package controller

import (
 "crypto/sha256"
 "fmt"
 "bytes"
 "html/template"
 "html"
 "net/http/httptest"
 "os"
 "path/filepath"
 "github.com/gin-gonic/gin"
 "os/exec"
 "strings"
 "testing"
)
func TestAgentInstallerCommand(t *testing.T){
 origin,ok:=installerOrigin("status.example.test:443");if !ok{t.Fatal("valid host rejected")}
 for _,s:=range []string{"", "x/y", "a@b", "x?z","x\r\n", "x:99999"}{if _,ok:=installerOrigin(s);ok{t.Fatal("invalid origin",s)}}
 secret:="a'b$z"
 cmd:=agentInstallCommand(origin,"agent.example.test",443,secret)
 if !strings.Contains(cmd,fmt.Sprintf("%x",sha256.Sum256(agentInstaller)))||!strings.Contains(cmd,"https://status.example.test:443/agent-install.sh")||!strings.Contains(cmd,shellQuote(secret))||strings.Contains(cmd,"disable-auto-update"){t.Fatal("invalid command")}
 if err:=exec.Command("bash","-n","-c",cmd).Run();err!=nil{t.Fatal(err)}
 check:=exec.Command("bash","-n");check.Stdin=strings.NewReader(string(agentInstaller));if err:=check.Run();err!=nil{t.Fatal(err)}
 if agentInstallCommand(origin,"",443,secret)!=""{t.Fatal("missing endpoint accepted")}
}

func TestAgentInstallerHTTP(t *testing.T){
 router:=gin.New();router.GET("/agent-install.sh",serveAgentInstaller)
 digest:=agentInstallerDigest()
 for _,query:=range []string{"","?sha256="+digest}{
  rec:=httptest.NewRecorder()
  router.ServeHTTP(rec,httptest.NewRequest("GET","/agent-install.sh"+query,nil))
  if rec.Code!=200||rec.Header().Get("X-Agent-Installer-SHA256")!=digest||!bytes.Equal(rec.Body.Bytes(),agentInstaller){t.Fatal("served installer differs from embedded bytes")}
 }
 rec:=httptest.NewRecorder()
 router.ServeHTTP(rec,httptest.NewRequest("GET","/agent-install.sh?sha256="+strings.Repeat("0",64),nil))
 if rec.Code!=409||rec.Body.Len()!=0{t.Fatal("stale installer digest accepted")}
}
func TestAgentInstallerClipboardRoundTrip(t *testing.T){
 secret:="a'b$z&<>"
 cmd:=agentInstallCommand("https://status.example.test","agent.example.test",443,secret)
 tpl:=template.Must(template.New("copy").Parse(`<button data-clipboard-text="{{.}}">Install</button>`))
 var page bytes.Buffer
 if err:=tpl.Execute(&page,cmd);err!=nil{t.Fatal(err)}
 rendered:=strings.TrimPrefix(page.String(),`<button data-clipboard-text="`)
 attr,_,ok:=strings.Cut(rendered,`">Install</button>`)
 if !ok||html.UnescapeString(attr)!=cmd{t.Fatal("clipboard HTML escaping changes shell command")}
 // Exercise the exact clipboard value using the real shell, a local curl
 // fixture and a harmless bash stand-in. Never run installation as root.
 shell,err:=exec.LookPath("bash");if err!=nil{t.Fatal(err)}
 dir:=t.TempDir()
 put:=func(name,body string,mode os.FileMode){t.Helper();if err:=os.WriteFile(filepath.Join(dir,name),[]byte(body),mode);err!=nil{t.Fatal(err)}}
 put("installer",string(agentInstaller),0600)
 put("curl",`#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
 if [ "$1" = "-o" ]; then shift; dest=$1; fi
 shift
done
cp "$FIXTURE/installer" "$dest"
`,0700)
 put("bash",`#!/bin/sh
set -eu
IFS= read -r secret
printf '%s\n%s\n' "$secret" "$2" > "$FIXTURE/result"
`,0700)
 run:=func()error{p:=exec.Command(shell,"-c",html.UnescapeString(attr));p.Env=append(os.Environ(),"PATH="+dir+":"+os.Getenv("PATH"),"FIXTURE="+dir);return p.Run()}
 if err:=run();err!=nil{t.Fatal("verified clipboard command failed",err)}
 got,err:=os.ReadFile(filepath.Join(dir,"result"))
 if err!=nil||string(got)!=secret+"\nagent.example.test:443\n"{t.Fatal("clipboard arguments changed")}
 if err:=os.Remove(filepath.Join(dir,"result"));err!=nil{t.Fatal(err)}
 put("installer","tampered",0600)
 if err:=run();err==nil{t.Fatal("tampered script executed")}
 if _,err:=os.Stat(filepath.Join(dir,"result"));!os.IsNotExist(err){t.Fatal("tampered script reached execution")}
}

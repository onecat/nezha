package controller

import(
 "crypto/sha256"
 "fmt"
 "bytes"
 "html/template"
 "html"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"
)
func TestAgentInstallerClipboardRoundTrip(t *testing.T){
 old:=agentInstallSHA256;defer func(){agentInstallSHA256=old}()
 agentInstallSHA256=""
 if agentInstallCommand("agent.example.test",443,"secret")!=""{t.Fatal("unverified release enabled")}
 // Harmless executable fixture, not a production installer.
 fixture:=`#!/bin/sh
set -eu
[ "$1" = service ] && [ "$2" = install ] && [ "$3" = --server ] && [ "$5" = --ca-file ]
IFS= read -r secret
printf '%s\n%s\n' "$secret" "$4" > "$FIXTURE/result"
`
 agentInstallSHA256=fmt.Sprintf("%x",sha256.Sum256([]byte(fixture)))
 secret:="a'b$z&<>"
 cmd:=agentInstallCommand("agent.example.test",443,secret)
 if !strings.Contains(cmd,agentInstallURL)||!strings.Contains(cmd,agentInstallSHA256)||strings.Contains(cmd,"agent-install.sh"){t.Fatal("invalid command")}
 if agentInstallCommand("",443,secret)!=""{t.Fatal("missing endpoint accepted")}
 tpl:=template.Must(template.New("copy").Parse(`<button data-clipboard-text="{{.}}">Install</button>`))
 var page bytes.Buffer
 if err:=tpl.Execute(&page,cmd);err!=nil{t.Fatal(err)}
 rendered:=strings.TrimPrefix(page.String(),`<button data-clipboard-text="`)
 attr,_,ok:=strings.Cut(rendered,`">Install</button>`)
 if !ok||html.UnescapeString(attr)!=cmd{t.Fatal("clipboard HTML escaping changes shell command")}
 shell,err:=exec.LookPath("sh");if err!=nil{t.Fatal(err)}
 dir:=t.TempDir()
 put:=func(name,body string,mode os.FileMode){t.Helper();if err:=os.WriteFile(filepath.Join(dir,name),[]byte(body),mode);err!=nil{t.Fatal(err)}}
 put("installer",fixture,0600)
 put("curl",`#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
 if [ "$1" = "-o" ]; then shift; dest=$1; fi
 shift
done
cp "$FIXTURE/installer" "$dest"
`,0700)
 run:=func()error{p:=exec.Command(shell,"-c",html.UnescapeString(attr));p.Env=append(os.Environ(),"PATH="+dir+":"+os.Getenv("PATH"),"FIXTURE="+dir);return p.Run()}
 if err:=run();err!=nil{t.Fatal("verified clipboard command failed",err)}
 got,err:=os.ReadFile(filepath.Join(dir,"result"))
 if err!=nil||string(got)!=secret+"\nagent.example.test:443\n"{t.Fatal("clipboard arguments changed")}
 if err:=os.Remove(filepath.Join(dir,"result"));err!=nil{t.Fatal(err)}
 put("installer","tampered",0600)
 if err:=run();err==nil{t.Fatal("tampered binary executed")}
 if _,err:=os.Stat(filepath.Join(dir,"result"));!os.IsNotExist(err){t.Fatal("tampered binary reached execution")}
}

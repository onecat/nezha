package controller

import (
 "crypto/sha256"
 "fmt"
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

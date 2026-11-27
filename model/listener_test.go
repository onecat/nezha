package model

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
 "golang.org/x/crypto/bcrypt"
)
func TestListenerAddresses(t *testing.T){
 for _,v:=range []struct{host,normal,managed string}{
  {"",":5555","127.0.0.1:5555"},
  {"127.0.0.1","127.0.0.1:5555","127.0.0.1:5555"},
  {"::1","[::1]:5555","[::1]:5555"},
  {"0.0.0.0","0.0.0.0:5555",""},
  {"::","[::]:5555",""},
  {"192.0.2.1","192.0.2.1:5555",""},
 }{
  c:=Config{HTTPBindHost:v.host,GRPCBindHost:v.host,HTTPPort:5555,GRPCPort:5555}
  if err:=c.ValidateListeners();err!=nil{t.Fatal(err)}
  if got:=c.HTTPListenAddress(5555);got!=v.normal{t.Fatal(got,v.normal)}
  if got,err:=c.GRPCListenAddress(5555,false);err!=nil||got!=v.normal{t.Fatal(got,err)}
  got,err:=c.GRPCListenAddress(5555,true)
  if v.managed==""{if err==nil{t.Fatal("non-loopback managed bind accepted")}}else if err!=nil||got!=v.managed{t.Fatal(got,err)}
 }
 for _,host:=range []string{"localhost","127.0.0.1:5555","[::1]","::1%lo"," 127.0.0.1","https://127.0.0.1"}{
  c:=Config{HTTPBindHost:host}
  if c.ValidateListeners()==nil{t.Fatal("invalid HTTP host accepted",host)}
  c=Config{GRPCBindHost:host}
  if c.ValidateListeners()==nil{t.Fatal("invalid gRPC host accepted",host)}
 }
}
func TestListenerConfigReadSave(t *testing.T){
 hash,err:=bcrypt.GenerateFromPassword([]byte("test-only"),bcrypt.MinCost)
 if err!=nil{t.Fatal(err)}
 path:=filepath.Join(t.TempDir(),"config.yaml")
 content:="HTTPPort: 8008\nGRPCPort: 5555\nHTTPBindHost: 127.0.0.1\nGRPCBindHost: '::1'\nGRPCHost: agent.example.invalid\nlocalauth:\n  username: admin\n  passwordhash: '"+string(hash)+"'\noauth2:\n  type: local\n  ClientID: obsolete-id\n  ClientSecret: obsolete-secret\n"
 if err:=os.WriteFile(path,[]byte(content),0600);err!=nil{t.Fatal(err)}
 var c Config
 if err:=c.Read(path);err!=nil{t.Fatal(err)}
 if c.HTTPBindHost!="127.0.0.1"||c.GRPCBindHost!="::1"||c.GRPCHost!="agent.example.invalid"{t.Fatal("legacy case or advertised host lost")}
 if err:=c.Save();err!=nil{t.Fatal(err)}
 b,err:=os.ReadFile(path);if err!=nil{t.Fatal(err)}
 for _,s:=range []string{"clientid:","clientsecret:","oidcissuer:","obsolete-secret","obsolete-id"}{if strings.Contains(string(b),s){t.Fatal("obsolete OAuth field serialized",s)}}
 var reread Config
 if err:=reread.Read(path);err!=nil{t.Fatal(err)}
 if reread.HTTPBindHost!=c.HTTPBindHost||reread.GRPCBindHost!=c.GRPCBindHost||reread.LocalAuth.PasswordHash!=string(hash){t.Fatal("save roundtrip failed")}
 // Original configs without host keys retain wildcard behavior.
 if err:=os.WriteFile(path,[]byte("localauth:\n  username: admin\n  passwordhash: '"+string(hash)+"'\n"),0600);err!=nil{t.Fatal(err)}
 var legacy Config
 if err:=legacy.Read(path);err!=nil{t.Fatal(err)}
 if legacy.HTTPListenAddress(legacy.HTTPPort)!=":80"{t.Fatal("legacy HTTP default changed")}
 if got,err:=legacy.GRPCListenAddress(legacy.GRPCPort,false);err!=nil||got!=":5555"{t.Fatal(got,err)}
}

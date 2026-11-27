package agentupgrade

import (
 "bytes"
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "os"
 "path/filepath"
 "runtime"
 "strings"
 "testing"

 core "github.com/naiba/nezha/pkg/agentupgrade"
)

func TestPackageStorage(t *testing.T){
 if runtime.GOOS!="linux"{t.Skip("Linux store")}
 pub,priv,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 dir:=t.TempDir()
 if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)}
 p,err:=OpenPackages(dir,map[string]ed25519.PublicKey{"test":pub})
 if err!=nil{t.Fatal(err)}
 var b bytes.Buffer
 body:=[]byte("test fixture")
 m:=core.Manifest{Version:"v0.30.01",MinAgentVersion:"v0.30.00",OS:"linux",Arch:"amd64",SigningKeyID:"test",ReleaseSequence:2}
 if _,err:=core.BuildPackage(&b,bytes.NewReader(body),int64(len(body)),m,priv);err!=nil{t.Fatal(err)}
 v,err:=p.Import(context.Background(),bytes.NewReader(b.Bytes()))
 if err!=nil{t.Fatal(err)}
 again,err:=p.Import(context.Background(),bytes.NewReader(b.Bytes()))
 if err!=nil || again!=v{t.Fatal("duplicate import",err)}
 f,got,err:=p.Open(v.PackageSHA256)
 if err!=nil || got!=v{t.Fatal("reopen",err)}
 f.Close()
 if _,err:=p.Import(context.Background(),strings.NewReader("invalid"));err==nil{t.Fatal("accepted unsigned package")}
 if f,_,err:=p.Open("../escape");err==nil{f.Close();t.Fatal("accepted invalid path")}
 path:=filepath.Join(dir,v.PackageSHA256+".nzu")
 if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,[]byte("tampered"),0600);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(path,0400);err!=nil{t.Fatal(err)}
 if f,_,err:=p.Open(v.PackageSHA256);err==nil{f.Close();t.Fatal("accepted tampered file")}
 if _,err:=p.Import(context.Background(),bytes.NewReader(b.Bytes()));err==nil{t.Fatal("overwrote tampered object")}
 entries,err:=os.ReadDir(dir)
 if err!=nil || len(entries)!=1{t.Fatal("unexpected staging file",err)}
}

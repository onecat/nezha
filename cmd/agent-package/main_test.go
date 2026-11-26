package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/naiba/nezha/pkg/agentupgrade"
)

func TestOfflineTool(t *testing.T) {
	dir:=t.TempDir()
	pub,priv,err:=ed25519.GenerateKey(rand.Reader)
	if err!=nil { t.Fatal(err) }
	der,err:=x509.MarshalPKCS8PrivateKey(priv)
	if err!=nil { t.Fatal(err) }
	key:=filepath.Join(dir,"release.pem")
	if err:=os.WriteFile(key,pem.EncodeToMemory(&pem.Block{Type:"PRIVATE KEY",Bytes:der}),0600);err!=nil { t.Fatal(err) }
	binary:=filepath.Join(dir,"binary")
	if err:=os.WriteFile(binary,[]byte("test fixture"),0600);err!=nil { t.Fatal(err) }
	out:=filepath.Join(dir,"release.nzu")
	args:=[]string{"-binary",binary,"-out",out,"-key",key,"-version","v0.30.01","-min-version","v0.30.00","-os","linux","-arch","amd64","-key-id","test","-sequence","2"}
	if err:=run(args);err!=nil { t.Fatal(err) }
	before,err:=os.ReadFile(out)
	if err!=nil { t.Fatal(err) }
	if err:=run(args);err==nil { t.Fatal("overwrote existing package") }
	after,err:=os.ReadFile(out)
	if err!=nil || string(before)!=string(after) { t.Fatal("output changed") }
	f,err:=os.Open(out)
	if err!=nil { t.Fatal(err) }
	defer f.Close()
	if _,err:=agentupgrade.VerifyPackage(f,int64(len(after)),map[string]ed25519.PublicKey{"test":pub});err!=nil { t.Fatal(err) }
	if runtime.GOOS!="windows" {
		if err:=os.Chmod(key,0644);err!=nil { t.Fatal(err) }
		if _,err:=loadKey(key);err==nil { t.Fatal("accepted unrestricted key") }
	}
}

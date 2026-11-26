// Offline NZU signing utility. Never run it in the Dashboard service.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/naiba/nezha/pkg/agentupgrade"
)

func loadKey(path string)(ed25519.PrivateKey,error) {
	f,err:=os.Open(path)
	if err!=nil { return nil,err }
	defer f.Close()
	info,err:=f.Stat()
	if err!=nil { return nil,err }
	if !info.Mode().IsRegular() || info.Size()>16384 ||
		(runtime.GOOS!="windows" && info.Mode().Perm()&0077!=0) {
		return nil,errors.New("private key must be a regular file with restricted permissions")
	}
	raw,err:=io.ReadAll(io.LimitReader(f,16385))
	if err!=nil { return nil,err }
	defer clear(raw)
	if len(raw)>16384 { return nil,errors.New("key file too large") }
	block,rest:=pem.Decode(raw)
	if block==nil || block.Type!="PRIVATE KEY" || len(bytes.TrimSpace(rest))!=0 {
		return nil,errors.New("expected a single PKCS#8 PRIVATE KEY PEM block")
	}
	defer clear(block.Bytes)
	k,err:=x509.ParsePKCS8PrivateKey(block.Bytes)
	if err!=nil { return nil,errors.New("invalid PKCS#8 private key") }
	key,ok:=k.(ed25519.PrivateKey)
	if !ok { return nil,errors.New("expected Ed25519 private key") }
	return key,nil
}

func run(args []string) error {
	fs:=flag.NewFlagSet("agent-package",flag.ContinueOnError)
	var m agentupgrade.Manifest
	var input,output,keyPath string
	fs.StringVar(&input,"binary","","path to the trusted built Agent binary")
	fs.StringVar(&output,"out","","new NZU file (must not exist)")
	fs.StringVar(&keyPath,"key","","offline Ed25519 PKCS#8 PEM private key")
	fs.StringVar(&m.Version,"version","","target version")
	fs.StringVar(&m.MinAgentVersion,"min-version","","minimum installed version")
	fs.StringVar(&m.OS,"os","","target GOOS")
	fs.StringVar(&m.Arch,"arch","","target GOARCH")
	fs.StringVar(&m.SigningKeyID,"key-id","","trusted signing key ID")
	fs.Uint64Var(&m.ReleaseSequence,"sequence",0,"monotonically increasing release sequence")
	if err:=fs.Parse(args);err!=nil { return err }
	if fs.NArg()!=0 || input=="" || output=="" || keyPath=="" || filepath.Ext(output)!=".nzu" {
		return errors.New("required: -binary -out (.nzu) -key -version -min-version -os -arch -key-id -sequence")
	}
	key,err:=loadKey(keyPath)
	if err!=nil { return err }
	defer clear(key)
	in,err:=os.Open(input)
	if err!=nil { return err }
	defer in.Close()
	info,err:=in.Stat()
	if err!=nil { return err }
	if !info.Mode().IsRegular() { return errors.New("binary must be a regular file") }
	// Stage next to the output; publish only after an independent verification.
	// Link is exclusive and does not overwrite an existing file.
	tmp,err:=os.CreateTemp(filepath.Dir(output),".nzu-staging-*")
	if err!=nil { return err }
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	signed,err:=agentupgrade.BuildPackage(tmp,in,info.Size(),m,key)
	if err!=nil { return err }
	stat,err:=tmp.Stat()
	if err!=nil { return err }
	pub:=key.Public().(ed25519.PublicKey)
	verified,err:=agentupgrade.VerifyPackage(tmp,stat.Size(),map[string]ed25519.PublicKey{signed.SigningKeyID:pub})
	if err!=nil { return err }
	if verified.Manifest!=signed { return errors.New("self-verification manifest mismatch") }
	if err:=tmp.Sync();err!=nil { return err }
	if err:=tmp.Close();err!=nil { return err }
	if err:=os.Link(tmp.Name(),output);err!=nil { return fmt.Errorf("publish new package (requires hard-link support and unused output path): %w",err) }
	fmt.Printf("created %s: %s %s/%s SHA256=%s\n",output,signed.Version,signed.OS,signed.Arch,verified.PackageSHA256)
	return nil
}

func main(){
	if err:=run(os.Args[1:]);err!=nil {
		if errors.Is(err,flag.ErrHelp) { return }
		fmt.Fprintln(os.Stderr,err)
		os.Exit(1)
	}
}

package agentupgrade

import (
 "context"
 "crypto/ed25519"
 "encoding/hex"
 "errors"
 "io"
 "os"
 "path/filepath"
 "runtime"

 core "github.com/naiba/nezha/pkg/agentupgrade"
)

// Packages stores verified releases in an operator-owned, private directory.
// The directory and its ancestors must not be writable by untrusted users.
// The caller enforces upload authorization and disk quotas. No HTTP route is
// exposed here. Initial support is Linux Dashboard only.
type Packages struct {
 dir string
 keys map[string]ed25519.PublicKey
 gate chan struct{}
}

func OpenPackages(dir string,keys map[string]ed25519.PublicKey)(*Packages,error){
 if runtime.GOOS!="linux" {return nil,errors.New("package storage requires Linux")}
 if !filepath.IsAbs(dir) || len(keys)==0{return nil,errors.New("invalid package store configuration")}
 st,err:=os.Lstat(dir)
 if err!=nil || !st.IsDir() || st.Mode().Perm()&0077!=0{return nil,errors.New("package directory must be private and pre-created")}
 copyKeys:=make(map[string]ed25519.PublicKey,len(keys))
 for k,v:=range keys{
  if k=="" || len(v)!=ed25519.PublicKeySize{return nil,errors.New("invalid trusted signing key")}
  copyKeys[k]=append(ed25519.PublicKey(nil),v...)
 }
 return &Packages{dir:dir,keys:copyKeys,gate:make(chan struct{},1)},nil
}

type contextReader struct {ctx context.Context;r io.Reader}
func(r contextReader) Read(b []byte)(int,error){
 if err:=r.ctx.Err();err!=nil{return 0,err}
 return r.r.Read(b)
}

func(p *Packages) Import(ctx context.Context,src io.Reader)(core.VerifiedPackage,error){
 var zero core.VerifiedPackage
 if src==nil{return zero,errors.New("missing package")}
 select{
 case p.gate<-struct{}{}:defer func(){<-p.gate}()
 case <-ctx.Done():return zero,ctx.Err()
 default:return zero,errors.New("package upload already in progress")
 }
 if err:=ctx.Err();err!=nil{return zero,err}
 f,err:=os.CreateTemp(p.dir,".upload-")
 if err!=nil{return zero,err}
 defer os.Remove(f.Name())
 defer f.Close()
 n,err:=io.Copy(f,io.LimitReader(contextReader{ctx,src},core.MaxPackageSize+1))
 if err!=nil{return zero,err}
 if n>core.MaxPackageSize{return zero,errors.New("package exceeds size limit")}
 v,err:=core.VerifyPackage(f,n,p.keys)
 if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 if err:=f.Chmod(0400);err!=nil{return zero,err}
 if err:=f.Sync();err!=nil{return zero,err}
 if err:=f.Close();err!=nil{return zero,err}
 dest:=filepath.Join(p.dir,v.PackageSHA256+".nzu")
 // Hard link publishes a fully verified file without replacing an existing
 // digest. Never overwrite a corrupt or untrusted existing object.
 if err:=os.Link(f.Name(),dest);err!=nil{
  if !errors.Is(err,os.ErrExist){return zero,err}
  old,existing,err:=p.Open(v.PackageSHA256)
  if err!=nil{return zero,err}
  defer old.Close()
  if existing!=v{return zero,errors.New("existing package mismatch")}
 }
 d,err:=os.Open(p.dir)
 if err!=nil{return zero,err}
 defer d.Close()
 if err:=d.Sync();err!=nil{return zero,err}
 return v,nil
}

func validDigest(s string)bool{
 if len(s)!=64{return false}
 b,err:=hex.DecodeString(s)
 return err==nil && hex.EncodeToString(b)==s
}

// Open checks the digest and signature again and returns the same opened
// read-only descriptor for transfer. Caller closes it. A trusted private
// directory is required: local privileged modification is outside the model.
func(p *Packages) Open(digest string)(*os.File,core.VerifiedPackage,error){
 var zero core.VerifiedPackage
 if !validDigest(digest){return nil,zero,errors.New("invalid package digest")}
 path:=filepath.Join(p.dir,digest+".nzu")
 before,err:=os.Lstat(path)
 if err!=nil{return nil,zero,err}
 if !before.Mode().IsRegular() || before.Mode().Perm()&0377!=0{return nil,zero,errors.New("package must be read-only private file")}
 f,err:=os.Open(path)
 if err!=nil{return nil,zero,err}
 st,err:=f.Stat()
 if err!=nil || !os.SameFile(before,st) || !st.Mode().IsRegular(){
  f.Close();return nil,zero,errors.New("package changed while opening")
 }
 v,err:=core.VerifyPackage(f,st.Size(),p.keys)
 if err!=nil || v.PackageSHA256!=digest{
  f.Close();return nil,zero,errors.New("stored package verification failed")
 }
 return f,v,nil
}

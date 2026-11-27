package upgradecontrol
import("testing";"strings";"net/url";"os";"path/filepath";"bytes")
func TestURLReleaseBoundary(t *testing.T){
 valid:=releasePrefix+"v0.30.01/"+binaryName
 if v,e:=parseRelease(valid);e!=nil||v!="v0.30.01"{t.Fatal(v,e)}
 if v,e:=parseRelease(releasePrefix+"v0.30.03/nezha-agent");e!=nil||v!="v0.30.03"{t.Fatal(v,e)}
 if got,e:=checksum(strings.Repeat("a",64)+"  nezha-agent\n","nezha-agent");e!=nil||len(got)!=64{t.Fatal(got,e)}
 for _,s:=range []string{"http://github.com/onecat/nezha-agent/releases/download/v0.30.01/"+binaryName,valid+"?x=1",valid+"/../other",strings.Replace(valid,"onecat","other",1),strings.Replace(valid,binaryName,"nezha-supervisor-linux-amd64",1),"https://127.0.0.1/file","https://github.com.evil.example/file"}{if _,err:=parseRelease(s);err==nil{t.Fatal("accepted",s)}}
 for _,s:=range []string{"http://github.com/file","https://127.0.0.1/file","https://github.com.evil.example/file","https://user@github.com/file","https://github.com:8443/file"}{u,_:=url.Parse(s);if releaseHost(u){t.Fatal("redirect accepted",s)}}
 h:=strings.Repeat("a",64);if got,err:=checksum(h+"  "+binaryName+"\n",binaryName);err!=nil||got!=h{t.Fatal(got,err)}
 if _,err:=checksum(h+"  "+binaryName+"\n"+h+"  "+binaryName,binaryName);err==nil{t.Fatal("duplicate checksum accepted")}
}
func TestLocalSigningKeyStable(t *testing.T){
 root:=t.TempDir()
 a,err:=localSigningKey(root);if err!=nil{t.Fatal(err)}
 b,err:=localSigningKey(root);if err!=nil||!a.Equal(b){t.Fatal("signing key changed",err)}
}

func TestLocalSigningKeyRejectsUnsafeExistingSeed(t *testing.T){
 root:=t.TempDir()
 path:=filepath.Join(root,"signing.seed")
 if err:=os.WriteFile(path,bytes.Repeat([]byte{7},32),0644);err!=nil{t.Fatal(err)}
 if _,err:=localSigningKey(root);err==nil{t.Fatal("public seed accepted")}
 if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
 if _,err:=localSigningKey(root);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,[]byte("invalid"),0600);err!=nil{t.Fatal(err)}
 if _,err:=localSigningKey(root);err==nil{t.Fatal("invalid seed accepted")}
 got,err:=os.ReadFile(path);if err!=nil||string(got)!="invalid"{t.Fatal("existing seed overwritten",err)}
}

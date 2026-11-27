package upgradecontrol
import("testing";"strings";"net/url")
func TestURLReleaseBoundary(t *testing.T){
 valid:=releasePrefix+"v0.30.01/"+binaryName
 if v,e:=parseRelease(valid);e!=nil||v!="v0.30.01"{t.Fatal(v,e)}
 for _,s:=range []string{"http://github.com/onecat/nezha-agent/releases/download/v0.30.01/"+binaryName,valid+"?x=1",valid+"/../other",strings.Replace(valid,"onecat","other",1),strings.Replace(valid,binaryName,"nezha-supervisor-linux-amd64",1),"https://127.0.0.1/file","https://github.com.evil.example/file"}{if _,err:=parseRelease(s);err==nil{t.Fatal("accepted",s)}}
 for _,s:=range []string{"http://github.com/file","https://127.0.0.1/file","https://github.com.evil.example/file","https://user@github.com/file","https://github.com:8443/file"}{u,_:=url.Parse(s);if releaseHost(u){t.Fatal("redirect accepted",s)}}
 h:=strings.Repeat("a",64);if got,err:=checksum(h+"  "+binaryName+"\n");err!=nil||got!=h{t.Fatal(got,err)}
 if _,err:=checksum(h+"  "+binaryName+"\n"+h+"  "+binaryName);err==nil{t.Fatal("duplicate checksum accepted")}
}
func TestLocalSigningKeyStable(t *testing.T){
 root:=t.TempDir()
 a,err:=localSigningKey(root);if err!=nil{t.Fatal(err)}
 b,err:=localSigningKey(root);if err!=nil||!a.Equal(b){t.Fatal("signing key changed",err)}
}

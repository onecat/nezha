package utils

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestHybridFSRoot(t *testing.T) {
	root:=t.TempDir()
	outside:=filepath.Join(t.TempDir(),"private")
	if err:=os.WriteFile(outside,[]byte("private"),0600);err!=nil { t.Fatal(err) }
	if err:=os.WriteFile(filepath.Join(root,"allowed"),[]byte("ok"),0600);err!=nil { t.Fatal(err) }
	if err:=os.Symlink(outside,filepath.Join(root,"escape"));err!=nil { t.Fatal(err) }
	h,err:=NewHybridFS(embed.FS{},".",root)
	if err!=nil { t.Fatal(err) }
	data,err:=fs.ReadFile(h,"allowed")
	if err!=nil || string(data)!="ok" { t.Fatalf("allowed resource: %q %v",data,err) }
	for _,name:=range []string{"escape","../private","/private"} {
		if file,err:=h.Open(name);err==nil { file.Close();t.Fatalf("escaped root: %s",name) }
	}
}

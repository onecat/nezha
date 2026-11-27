package upgradecontrol

import(
 "context"
 "strings"
 "testing"
 "gorm.io/driver/sqlite"
 "gorm.io/gorm"
)
func TestPersistSelectionAndRevision(t *testing.T){
 db,err:=gorm.Open(sqlite.Open(t.TempDir()+"/selection.db"),&gorm.Config{});if err!=nil{t.Fatal(err)}
 if err:=db.AutoMigrate(&ReleaseSelection{});err!=nil{t.Fatal(err)}
 ctx:=context.Background()
 initial,err:=readSelection(ctx,db);if err!=nil||initial.Valid(){t.Fatal(initial,err)}
 r:=ReleaseSelection{URL:releasePrefix+"v0.99.01/nezha-agent",Version:"v0.99.01",SHA256:strings.Repeat("a",64)}
 first,err:=persistSelection(ctx,db,r);if err!=nil||!first.Valid()||first.Revision!=1{t.Fatal(first,err)}
 r.SHA256=strings.Repeat("b",64)
 if _,err:=persistSelection(ctx,db,r);err==nil{t.Fatal("mutable release accepted")}
 got,err:=readSelection(ctx,db);if err!=nil||got!=first{t.Fatal(got,err)}
 r.URL=releasePrefix+"v0.99.02/nezha-agent";r.Version="v0.99.02"
 next,err:=persistSelection(ctx,db,r);if err!=nil||next.Revision!=2{t.Fatal(next,err)}
 got,err=readSelection(ctx,db);if err!=nil||got!=next{t.Fatal(got,err)}
}
func TestForwardEligible(t *testing.T){
 if !forwardEligible("v0.30.04","v0.30.03",1){t.Fatal("forward upgrade refused")}
 for _,v:=range []string{"v0.30.04","v0.30.05","v0.20.5","invalid"}{if forwardEligible("v0.30.04",v,1){t.Fatal("ineligible version",v)}}
}

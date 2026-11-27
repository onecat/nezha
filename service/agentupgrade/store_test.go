package agentupgrade

import (
 "context"
 "errors"
 "path/filepath"
 "strings"
 "sync"
 "testing"
 "time"

 core "github.com/naiba/nezha/pkg/agentupgrade"
 "gorm.io/driver/sqlite"
 "gorm.io/gorm"
 "gorm.io/gorm/logger"
)

func openTest(t *testing.T,path string)*Store {
 t.Helper()
 db,err:=gorm.Open(sqlite.Open(path),&gorm.Config{Logger:logger.Default.LogMode(logger.Silent)})
 if err!=nil { t.Fatal(err) }
 sqlDB,err:=db.DB()
 if err!=nil { t.Fatal(err) }
 sqlDB.SetMaxOpenConns(1)
 t.Cleanup(func(){sqlDB.Close()})
 s,err:=Open(context.Background(),db)
 if err!=nil { t.Fatal(err) }
 return s
}

func fixture(id string,server uint64,now time.Time)core.Transaction {
 return core.Transaction{ID:id,ServerID:server,PackageSHA256:strings.Repeat("a",64),Version:"v0.30.01",ReleaseSequence:2,State:core.StatePending,ExpiresAt:now.Add(time.Hour)}
}

func TestPersistentTaskReservation(t *testing.T){
 ctx:=context.Background()
 now:=time.Now().UTC()
 path:=filepath.Join(t.TempDir(),"tasks.db")
 s:=openTest(t,path)
 first,err:=s.Create(ctx,fixture("first",1,now),now)
 if err!=nil { t.Fatal(err) }
 if _,err:=s.Create(ctx,fixture("second",1,now),now);err==nil { t.Fatal("duplicate active server") }
 s2:=openTest(t,path)
 got,err:=s2.Get(ctx,"first")
 if err!=nil || got.Revision!=1 || !got.Task.SameIdentity(first.Task) { t.Fatalf("reopen: %+v %v",got,err) }
 if _,err:=s2.Create(ctx,fixture("second",1,now),now);err==nil { t.Fatal("lost reservation after reopen") }
 next,err:=s2.Advance(ctx,got,core.StateTransferring,now)
 if err!=nil { t.Fatal(err) }
 if _,err:=s.Advance(ctx,first,core.StateFailed,now);!errors.Is(err,ErrConflict) { t.Fatalf("stale state: %v",err) }
 wrong:=next
 wrong.Task.ServerID=2
 if _,err:=s.Advance(ctx,wrong,core.StateFailed,now);!errors.Is(err,ErrConflict) { t.Fatal("substituted identity") }
 if _,err:=s.Advance(ctx,next,core.StateInstalling,now);err==nil { t.Fatal("illegal transition") }
 // Expiration does not unlock the server until a terminal transition.
 late:=now.Add(2*time.Hour)
 if _,err:=s2.Create(ctx,fixture("second",1,late),late);err==nil { t.Fatal("expiration released reservation") }
 if _,err:=s2.Advance(ctx,next,core.StateFailed,late);err!=nil { t.Fatal(err) }
 if _,err:=s.Create(ctx,fixture("first",2,late),late);err==nil { t.Fatal("reused transaction ID") }
 if _,err:=s.Create(ctx,fixture("second",1,late),late);err!=nil { t.Fatal(err) }
 page,err:=s.ActivePage(ctx,"",1)
 if err!=nil || len(page)!=1 || page[0].Task.ID!="second" { t.Fatalf("active page: %+v %v",page,err) }
}

func TestConcurrentTaskReservation(t *testing.T){
 ctx:=context.Background()
 now:=time.Now().UTC()
 s:=openTest(t,filepath.Join(t.TempDir(),"tasks.db"))
 var wg sync.WaitGroup
 results:=make(chan error,2)
 for _,id:=range []string{"a","b"}{
  wg.Add(1)
  go func(id string){defer wg.Done();_,err:=s.Create(ctx,fixture(id,1,now),now);results<-err}(id)
 }
 wg.Wait()
 close(results)
 successes:=0
 for err:=range results { if err==nil { successes++ } }
 if successes!=1 { t.Fatalf("got %d successful reservations",successes) }
}

func TestCorruptSnapshotFailsClosed(t *testing.T){
 ctx:=context.Background()
 now:=time.Now().UTC()
 s:=openTest(t,filepath.Join(t.TempDir(),"tasks.db"))
 r,err:=s.Create(ctx,fixture("task",1,now),now)
 if err!=nil { t.Fatal(err) }
 if err:=s.db.Model(&taskRow{}).Where("id = ?",r.Task.ID).Update("server_id",2).Error;err!=nil { t.Fatal(err) }
 if _,err:=s.Get(ctx,r.Task.ID);err==nil { t.Fatal("accepted corrupted identity") }
 if _,err:=s.ActivePage(ctx,"",10);err==nil { t.Fatal("accepted corrupted recovery record") }
}

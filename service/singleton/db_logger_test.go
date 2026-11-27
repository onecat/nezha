package singleton

import (
 "bytes"
 "log"
 "strings"
 "testing"

 "github.com/naiba/nezha/model"
 "gorm.io/driver/sqlite"
 "gorm.io/gorm"
)
func TestDatabaseLoggerRedactsParameters(t *testing.T){
 var out bytes.Buffer
 db,err:=gorm.Open(sqlite.Open(":memory:"),&gorm.Config{Logger:databaseLogger(log.New(&out,"",0))})
 if err!=nil{t.Fatal(err)}
 sqlDB,err:=db.DB();if err!=nil{t.Fatal(err)};defer sqlDB.Close()
 sqlDB.SetMaxOpenConns(1)
 if err:=db.AutoMigrate(&model.User{});err!=nil{t.Fatal(err)}
 out.Reset()
 token:="test-sensitive-session-token"
 var user model.User
 if err:=db.Where("token = ?",token).First(&user).Error;err!=gorm.ErrRecordNotFound{t.Fatal(err)}
 if out.Len()!=0{t.Fatal("expected missing user produced log",out.String())}
 if err:=db.Debug().Where("token = ?",token).First(&user).Error;err!=gorm.ErrRecordNotFound{t.Fatal(err)}
 if strings.Contains(out.String(),token){t.Fatal("debug log leaked token")}
 out.Reset()
 if err:=db.Exec("SELECT missing_column FROM users WHERE token = ?",token).Error;err==nil{t.Fatal("expected SQL error")}
 if !strings.Contains(out.String(),"missing_column")||strings.Contains(out.String(),token){t.Fatal("unexpected error lost or token leaked",out.String())}
}

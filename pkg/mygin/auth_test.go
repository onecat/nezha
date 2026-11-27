package mygin

import (
	"testing"
	"time"

	"github.com/naiba/nezha/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLookupAPIUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	first := model.User{Login: "first", SuperAdmin: true}
	owner := model.User{Login: "owner"}
	for _, u := range []*model.User{&first, &owner} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	if u, ok := lookupAPIUser(db, owner.ID); !ok || u.ID != owner.ID || u.SuperAdmin {
		t.Fatalf("API token mapped to wrong owner: %+v", u)
	}
	for _, id := range []uint64{0, owner.ID + 1000} {
		if _, ok := lookupAPIUser(db, id); ok {
			t.Fatalf("invalid owner %d accepted", id)
		}
	}
	if err := db.Delete(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok := lookupAPIUser(db, owner.ID); ok {
		t.Fatal("deleted owner accepted")
	}
}

func TestLookupSessionUser(t *testing.T) {
 db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
 if err != nil { t.Fatal(err) }
 sqlDB, err := db.DB()
 if err != nil { t.Fatal(err) }
 defer sqlDB.Close()
 sqlDB.SetMaxOpenConns(1)
 if err := db.AutoMigrate(&model.User{}); err != nil { t.Fatal(err) }
 user := model.User{Login:"local:admin", SuperAdmin:true, Token:"fixture-valid-session", TokenExpired:time.Now().Add(time.Hour)}
 if err := db.Create(&user).Error; err != nil { t.Fatal(err) }
 if got,ok := lookupSessionUser(db,user.Token); !ok || got.ID!=user.ID || !got.SuperAdmin || !got.TokenExpired.After(time.Now()) { t.Fatal("valid session not found") }
 for _, token := range []string{"","fixture-stale-session"} {
  if _,ok := lookupSessionUser(db,token); ok { t.Fatal("unknown session accepted") }
 }
 // Startup invalidation and credential rotation clear the stored token.
 old := user.Token
 if err:=db.Model(&user).Updates(map[string]any{"token":"","token_expired":time.Now()}).Error;err!=nil{t.Fatal(err)}
 if _,ok := lookupSessionUser(db,old);ok{t.Fatal("invalidated session accepted")}
 if err:=db.Model(&user).Update("token","fixture-deleted-session").Error;err!=nil{t.Fatal(err)}
 if err:=db.Delete(&user).Error;err!=nil{t.Fatal(err)}
 if _,ok:=lookupSessionUser(db,"fixture-deleted-session");ok{t.Fatal("deleted session accepted")}
}

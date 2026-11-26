package mygin

import (
	"testing"

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

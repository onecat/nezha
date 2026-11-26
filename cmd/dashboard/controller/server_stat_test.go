package controller

import (
	"testing"
	"github.com/gin-gonic/gin"
	"github.com/naiba/nezha/model"
	"github.com/naiba/nezha/pkg/utils"
	"github.com/naiba/nezha/service/singleton"
)

func TestEmptyServerStats(t *testing.T) {
	singleton.SortedServerLock.Lock()
	old := singleton.SortedServerListForGuest
	singleton.SortedServerListForGuest = nil
	singleton.SortedServerLock.Unlock()
	defer func(){ singleton.SortedServerLock.Lock(); singleton.SortedServerListForGuest=old; singleton.SortedServerLock.Unlock() }()
	cp:=&commonPage{}
	c:=&gin.Context{}
	for _, note:=range []bool{true,false} {
		raw,err:=cp.getServerStat(c,note)
		if err!=nil { t.Fatal(err) }
		var got struct { Servers []*model.Server }
		if err:=utils.Json.Unmarshal(raw,&got);err!=nil { t.Fatal(err) }
		if got.Servers==nil || len(got.Servers)!=0 { t.Fatalf("expected empty array: %s",raw) }
	}
}

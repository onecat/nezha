package controller

import ("testing";"github.com/gin-gonic/gin";"github.com/naiba/nezha/model";"github.com/naiba/nezha/service/singleton")

func TestNetworkAccess(t *testing.T) {
	singleton.ServerLock.Lock()
	old:=singleton.ServerList
	singleton.ServerList=map[uint64]*model.Server{1:{},2:{HideForGuest:true}}
	singleton.ServerLock.Unlock()
	defer func(){singleton.ServerLock.Lock();singleton.ServerList=old;singleton.ServerLock.Unlock()}()
	c:=&gin.Context{}
	if !canViewServer(c,1) || canViewServer(c,2) || canViewServer(c,3) {t.Fatal("guest access policy")}
	c.Set(model.CtxKeyViewPasswordVerified,true)
	if !canViewServer(c,2) || canViewServer(c,3) {t.Fatal("verified view access policy")}
	admin:=&gin.Context{}
	admin.Set(model.CtxKeyAuthorizedUser,&model.User{})
	if !canViewServer(admin,2) {t.Fatal("admin access rejected")}
}

package controller

import (
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/naiba/nezha/pkg/mygin"
)

type guestPage struct {
	r *gin.Engine
}

func (gp *guestPage) serve() {
	gr := gp.r.Group("")
	gr.Use(mygin.Authorize(mygin.AuthorizeOption{
		GuestOnly: true, IsPage: true, Msg: "您已登录", Btn: "返回首页", Redirect: "/",
	}))
	gr.GET("/login", gp.login)
	gr.POST("/local/login", gp.localLogin)
}

func (gp *guestPage) login(c *gin.Context) {
	token, err := newLocalCSRF(c)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.HTML(http.StatusOK, "dashboard-default/login", mygin.CommonEnvironment(c, gin.H{
		"Title": "管理员登录", "CSRFToken": token, "LoginError": c.Query("error") == "1",
	}))
}

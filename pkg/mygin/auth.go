package mygin

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/naiba/nezha/model"
	"github.com/naiba/nezha/service/singleton"
)

type AuthorizeOption struct {
	GuestOnly  bool
	MemberOnly bool
	IsPage     bool
	AllowAPI   bool
	Msg        string
	Redirect   string
	Btn        string
}

func Authorize(opt AuthorizeOption) func(*gin.Context) {
	return func(c *gin.Context) {
		var code = http.StatusForbidden
		if opt.GuestOnly {
			code = http.StatusBadRequest
		}

		commonErr := ErrInfo{
			Title: "访问受限",
			Code:  code,
			Msg:   opt.Msg,
			Link:  opt.Redirect,
			Btn:   opt.Btn,
		}
		var isLogin bool

		// 用户鉴权
		token, _ := c.Cookie(singleton.Conf.Site.CookieName)
		token = strings.TrimSpace(token)
		if token != "" {
			var u model.User
			if err := singleton.DB.Where("token = ?", token).First(&u).Error; err == nil {
				isLogin = u.TokenExpired.After(time.Now())
				if isLogin {
					if singleton.Conf.Oauth2.Type == model.ConfigTypeLocal {
						isLogin = u.Login == "local:"+singleton.Conf.LocalAuth.Username
					} else if strings.HasPrefix(u.Login, "local:") {
						isLogin = false
					}
				}
			}
			if isLogin {
				c.Set(model.CtxKeyAuthorizedUser, &u)
			}
		}

		// API tokens authenticate their actual owner. A valid browser session
		// takes precedence over Authorization (which may contain proxy Basic auth).
		if opt.AllowAPI && !isLogin {
			apiToken := c.GetHeader("Authorization")
			if apiToken != "" {
				singleton.ApiLock.RLock()
				token, ok := singleton.ApiTokenList[apiToken]
				var userID uint64
				if ok && token != nil {
					userID = token.UserID
				}
				singleton.ApiLock.RUnlock()
				if u, ok := lookupAPIUser(singleton.DB, userID); ok {
					isLogin = true
					c.Set(model.CtxKeyAuthorizedUser, &u)
					c.Set("isAPI", true)
				}
			}
		}

		// 已登录且只能游客访问
		if isLogin && opt.GuestOnly {
			ShowErrorPage(c, commonErr, opt.IsPage)
			return
		}

		// 未登录且需要登录
		if !isLogin && opt.MemberOnly {
			ShowErrorPage(c, commonErr, opt.IsPage)
			return
		}
	}
}

func lookupAPIUser(db *gorm.DB, userID uint64) (model.User, bool) {
	var user model.User
	if userID == 0 {
		return user, false
	}
	err := db.Where("id = ?", userID).First(&user).Error
	return user, err == nil
}

package mygin

import (
	"net/http"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/naiba/nezha/model"
	"github.com/naiba/nezha/service/singleton"
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

const ViewPasswordAge = 24 * time.Hour

// A process-local secret makes view-password cookies cheap to verify and
// invalidates old cookies on restart without changing the database.
var viewPasswordKey = func() [32]byte {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil { panic(err) }
	return key
}()

func viewPasswordMAC(expiry []byte, password string) []byte {
	mac := hmac.New(sha256.New, viewPasswordKey[:])
	mac.Write([]byte("nezha-view-password-v1"))
	mac.Write(expiry)
	mac.Write([]byte(password))
	return mac.Sum(nil)
}

func NewViewPasswordToken(password string, now time.Time) string {
	var data [8+sha256.Size]byte
	binary.BigEndian.PutUint64(data[:8], uint64(now.Add(ViewPasswordAge).Unix()))
	copy(data[8:], viewPasswordMAC(data[:8], password))
	return base64.RawURLEncoding.EncodeToString(data[:])
}

func validViewPasswordToken(token, password string, now time.Time) bool {
	if len(token) != base64.RawURLEncoding.EncodedLen(8+sha256.Size) { return false }
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(data)!=8+sha256.Size { return false }
	exp := binary.BigEndian.Uint64(data[:8])
	if exp <= uint64(now.Unix()) || exp > uint64(now.Add(ViewPasswordAge).Unix()) { return false }
	return hmac.Equal(data[8:], viewPasswordMAC(data[:8],password))
}

type ValidateViewPasswordOption struct {
	IsPage        bool
	AbortWhenFail bool
}

func ValidateViewPassword(opt ValidateViewPasswordOption) gin.HandlerFunc {
	return func(c *gin.Context) {
		if singleton.Conf.Site.ViewPassword == "" {
			return
		}
		_, authorized := c.Get(model.CtxKeyAuthorizedUser)
		if authorized {
			return
		}
		viewPassword, err := c.Cookie(singleton.Conf.Site.CookieName + "-vp")
		if err == nil && validViewPasswordToken(viewPassword, singleton.Conf.Site.ViewPassword, time.Now()) {
			c.Set(model.CtxKeyViewPasswordVerified, true)
			return
		}
		if !opt.AbortWhenFail {
			return
		}
		if opt.IsPage {
			c.HTML(http.StatusOK, GetPreferredTheme(c, "/viewpassword"), CommonEnvironment(c, gin.H{
				"Title": singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "VerifyPassword"}),
			}))

		} else {
			c.JSON(http.StatusOK, model.Response{
				Code:    http.StatusForbidden,
				Message: "访问受限",
			})
		}
		c.Abort()
	}
}

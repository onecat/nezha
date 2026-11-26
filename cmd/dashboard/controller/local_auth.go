package controller

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/naiba/nezha/model"
	"github.com/naiba/nezha/pkg/utils"
	"github.com/naiba/nezha/service/singleton"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	localCSRFExpires = 300
	localSessionAge  = 24 * time.Hour
	localLoginWindow = 15 * time.Minute
	localLoginLimit  = 5
)

var localAttempts = struct {
	sync.Mutex
	failures map[string]localFailure
}{failures: make(map[string]localFailure)}

var localUserMu sync.Mutex
var localPasswordSlots = make(chan struct{}, 4)

type localFailure struct {
	Count int
	Start time.Time
}

func localRemoteIP(c *gin.Context) string {
	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	// Only a same-machine reverse proxy may provide trusted nz-realip.
	if ip == "127.0.0.1" || ip == "::1" {
		if forwarded := net.ParseIP(c.GetHeader("nz-realip")); forwarded != nil {
			return forwarded.String()
		}
	}
	return ip
}

// Reserve an attempt before performing bcrypt so parallel guesses cannot bypass the limit.
func localReserveAttempt(key string) bool {
	localAttempts.Lock()
	defer localAttempts.Unlock()
	now := time.Now()
	f, ok := localAttempts.failures[key]
	if !ok || now.Sub(f.Start) >= localLoginWindow {
		if len(localAttempts.failures) >= 4096 {
			for addr, old := range localAttempts.failures {
				if now.Sub(old.Start) >= localLoginWindow {
					delete(localAttempts.failures, addr)
				}
			}
			if len(localAttempts.failures) >= 4096 {
				return false
			}
		}
		f = localFailure{Start: now}
	}
	if f.Count >= localLoginLimit {
		return false
	}
	f.Count++
	localAttempts.failures[key] = f
	return true
}

func localResetAttempts(key string) {
	localAttempts.Lock()
	defer localAttempts.Unlock()
	delete(localAttempts.failures, key)
}

func localSecureCookie(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback() &&
		c.GetHeader("X-Forwarded-Proto") == "https"
}

func newLocalCSRF(c *gin.Context) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(singleton.Conf.Site.CookieName+"-local-csrf", token,
		localCSRFExpires, "/", "", localSecureCookie(c), true)
	return token, nil
}

func localSameOrigin(c *gin.Context) bool {
	if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := c.GetHeader("Origin")
	if origin == "" {
		return true // csrf cookie and form token are still required
	}
	u, err := url.Parse(origin)
	scheme := "http"
	if localSecureCookie(c) {
		scheme = "https"
	}
	return err == nil && u.Host == c.Request.Host && u.Scheme == scheme &&
		u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

type localLoginForm struct {
	Username string `form:"username"`
	Password string `form:"password"`
	CSRF string `form:"csrf_token"`
}

func (gp *guestPage) localLogin(c *gin.Context) {
	if singleton.Conf.Oauth2.Type != model.ConfigTypeLocal {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var input localLoginForm
	csrfCookie, cookieErr := c.Cookie(singleton.Conf.Site.CookieName + "-local-csrf")
	if err := c.ShouldBind(&input); err != nil || cookieErr != nil ||
		len(input.CSRF) < 32 || len(csrfCookie) != len(input.CSRF) ||
		subtle.ConstantTimeCompare([]byte(input.CSRF), []byte(csrfCookie)) != 1 ||
		!localSameOrigin(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	client := localRemoteIP(c)
	select {
	case localPasswordSlots <- struct{}{}:
		defer func() { <-localPasswordSlots }()
	default:
		c.AbortWithStatus(http.StatusTooManyRequests)
		return
	}
	if !localReserveAttempt(client) {
		c.AbortWithStatus(http.StatusTooManyRequests)
		return
	}
	validPassword := bcrypt.CompareHashAndPassword([]byte(singleton.Conf.LocalAuth.PasswordHash), []byte(input.Password)) == nil
	validUser := strings.TrimSpace(input.Username) == singleton.Conf.LocalAuth.Username
	if !validUser || !validPassword {
		c.Redirect(http.StatusSeeOther, "/login?error=1")
		return
	}
	login := "local:" + singleton.Conf.LocalAuth.Username
	localUserMu.Lock()
	defer localUserMu.Unlock()
	var user model.User
	err := singleton.DB.Where("login = ?", login).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		user = model.User{Login: login, Name: singleton.Conf.LocalAuth.Username, SuperAdmin: true}
		if err = singleton.DB.Create(&user).Error; err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
	} else if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	token, err := utils.GenerateRandomString(32)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	user.Token = token
	user.TokenExpired = time.Now().Add(localSessionAge)
	if err := singleton.DB.Save(&user).Error; err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	localResetAttempts(client)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(singleton.Conf.Site.CookieName, token, int(localSessionAge.Seconds()),
		"/", "", localSecureCookie(c), true)
	c.Redirect(http.StatusSeeOther, "/server")
}

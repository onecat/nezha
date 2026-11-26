package controller

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// Authenticated browser mutations require proof of the exact dashboard origin.
// An explicit authenticated API token is not an ambient browser credential.
func requireSameOriginMutation(c *gin.Context) {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return
	}
	if c.GetBool("isAPI") {
		return
	}
	if !sameOriginMutation(c) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "请求来源校验失败"})
	}
}

func sameOriginMutation(c *gin.Context) bool {
	if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	scheme := "http"
	if localSecureCookie(c) {
		scheme = "https"
	}
	origin := c.GetHeader("Origin")
	isOrigin := origin != ""
	if !isOrigin {
		origin = c.GetHeader("Referer")
	}
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.Scheme != scheme ||
		!strings.EqualFold(u.Host, c.Request.Host) || u.Host == "" {
		return false
	}
	return !isOrigin || (u.Path == "" && u.RawQuery == "" && u.Fragment == "")
}

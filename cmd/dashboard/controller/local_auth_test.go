package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func localTestContext() *gin.Context {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest("POST", "https://status.catdns.org/local/login", nil)
	req.Host = "status.catdns.org"
	req.Header.Set("X-Forwarded-Proto", "https")
	ctx.Request = req
	return ctx
}

func TestLocalSameOrigin(t *testing.T) {
	ctx := localTestContext()
	ctx.Request.Header.Set("Origin", "https://status.catdns.org")
	if !localSameOrigin(ctx) { t.Fatal("expected matching origin") }
	ctx.Request.Header.Set("Origin", "https://evil.example")
	if localSameOrigin(ctx) { t.Fatal("mismatched host accepted") }
	ctx.Request.Header.Set("Origin", "http://status.catdns.org")
	if localSameOrigin(ctx) { t.Fatal("mismatched scheme accepted") }
	ctx.Request.Header.Set("Origin", "https://status.catdns.org")
	ctx.Request.Header.Set("Sec-Fetch-Site", "cross-site")
	if localSameOrigin(ctx) { t.Fatal("cross-site request accepted") }
}

func TestLocalRemoteIP(t *testing.T) {
	ctx := localTestContext()
	ctx.Request.RemoteAddr = "203.0.113.10:4567"
	ctx.Request.Header.Set("nz-realip", "198.51.100.2")
	if got:=localRemoteIP(ctx); got!="203.0.113.10" {t.Fatal(got)}
	ctx.Request.RemoteAddr="127.0.0.1:4567"
	if got:=localRemoteIP(ctx); got!="198.51.100.2" {t.Fatal(got)}
}

func TestLocalRateLimit(t *testing.T) {
	ip:="test-local-auth-rate-limit"
	defer localRecordAttempt(ip,true)
	localRecordAttempt(ip,true)
	for i:=0;i<localLoginLimit;i++ {
		if localTooManyAttempts(ip) { t.Fatalf("premature block at %d",i) }
		localRecordAttempt(ip,false)
	}
	if !localTooManyAttempts(ip) {t.Fatal("limit not enforced")}
	localRecordAttempt(ip,true)
	if localTooManyAttempts(ip) {t.Fatal("successful login does not reset")}
}

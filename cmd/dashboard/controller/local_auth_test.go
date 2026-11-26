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
	ip := "test-local-auth-rate-limit"
	defer localResetAttempts(ip)
	localResetAttempts(ip)
	for i := 0; i < localLoginLimit; i++ {
		if !localReserveAttempt(ip) { t.Fatalf("premature block at %d", i) }
	}
	if localReserveAttempt(ip) { t.Fatal("limit not enforced") }
	localResetAttempts(ip)
	if !localReserveAttempt(ip) { t.Fatal("successful login does not reset") }
}

func TestLocalRateLimitConcurrent(t *testing.T) {
	ip := "test-local-auth-concurrent"
	defer localResetAttempts(ip)
	results := make(chan bool, 50)
	for i := 0; i < cap(results); i++ {
		go func() { results <- localReserveAttempt(ip) }()
	}
	accepted := 0
	for i := 0; i < cap(results); i++ {
		if <-results { accepted++ }
	}
	if accepted != localLoginLimit { t.Fatalf("accepted %d concurrent attempts", accepted) }
}

func TestLocalSecureCookieProxy(t *testing.T) {
	ctx := localTestContext()
	ctx.Request.TLS = nil
	ctx.Request.RemoteAddr = "203.0.113.10:1234"
	if localSecureCookie(ctx) { t.Fatal("untrusted forwarded proto accepted") }
	ctx.Request.RemoteAddr = "127.0.0.1:1234"
	if !localSecureCookie(ctx) { t.Fatal("trusted loopback proxy rejected") }
}

package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMutationOrigin(t *testing.T) {
	for _, tc := range []struct{
		name, origin, referer, fetch string
		want bool
	}{
		{"same", "https://status.example", "", "same-origin", true},
		{"referer", "", "https://status.example/server", "", true},
		{"cross-origin", "https://other.example", "", "", false},
		{"different-scheme", "http://status.example", "", "", false},
		{"same-site-cross-origin", "https://other.status.example", "", "same-site", false},
		{"null", "null", "https://status.example/", "", false},
		{"missing", "", "", "", false},
		{"fetch-cross-site", "https://status.example", "", "cross-site", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "https://status.example/api/cron/1/manual", nil)
			ctx.Request.Header.Set("Origin", tc.origin)
			ctx.Request.Header.Set("Referer", tc.referer)
			ctx.Request.Header.Set("Sec-Fetch-Site", tc.fetch)
			if got := sameOriginMutation(ctx); got != tc.want { t.Fatalf("got %v want %v", got, tc.want) }
		})
	}
}

func TestMutationMiddleware(t *testing.T) {
	for _, tc := range []struct{method string; token bool; want int}{
		{http.MethodGet, false, 204},
		{http.MethodPost, false, 403},
		{http.MethodPost, true, 204},
	} {
		r := gin.New()
		r.Use(func(c *gin.Context) { if tc.token { c.Set("isAPI", true) } })
		r.Use(requireSameOriginMutation)
		r.Any("/x", func(c *gin.Context) { c.Status(204) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, "https://status.example/x", nil))
		if w.Code != tc.want { t.Fatalf("%s token=%v: got %d want %d", tc.method, tc.token, w.Code, tc.want) }
	}
}

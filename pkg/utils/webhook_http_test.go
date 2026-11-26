package utils

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestWebhookAddressPolicy(t *testing.T) {
	p := webhookPolicy{allowed: []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")}}
	for _, tc:=range []struct{ip string; want bool}{
		{"8.8.8.8",true},{"2606:4700:4700::1111",true},{"10.20.1.2",true},
		{"10.21.1.2",false},{"127.0.0.1",false},{"::ffff:127.0.0.1",false},
		{"169.254.169.254",false},{"100.100.100.200",false},{"fd00:ec2::254",false},
		{"192.168.1.1",false},{"::1",false},{"fc00::1",false},{"0.0.0.0",false},
		{"224.0.0.1",false},{"64:ff9b::a00:1",false},
	} { if p.permits(netip.MustParseAddr(tc.ip))!=tc.want { t.Errorf("unexpected policy for %s",tc.ip) } }
}

func TestWebhookLocalAndRedirect(t *testing.T) {
	target:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ w.WriteHeader(204) }))
	defer target.Close()
	blocked:=webhookPolicy{}.client(true)
	if resp,err:=blocked.Get(target.URL); err==nil { resp.Body.Close(); t.Fatal("loopback allowed without policy") }
	allowed:=webhookPolicy{allowed:[]netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}.client(true)
	resp,err:=allowed.Get(target.URL)
	if err!=nil { t.Fatal(err) }; resp.Body.Close()
	redirect:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ http.Redirect(w,r,target.URL,302) }))
	defer redirect.Close()
	if resp,err:=allowed.Get(redirect.URL);err==nil { resp.Body.Close();t.Fatal("cross-origin redirect accepted") }
}

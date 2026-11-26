package utils

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"
)

type webhookPolicy struct { allowed []netip.Prefix }
type webhookClients struct { verified, unverified *http.Client }
var activeWebhookClients atomic.Pointer[webhookClients]

var specialNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func (p webhookPolicy) permits(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsMulticast() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.Zone() != "" { return false }
	// Cloud metadata must remain blocked even with broad private allowlists.
	if addr == netip.MustParseAddr("fd00:ec2::254") || addr == netip.MustParseAddr("100.100.100.200") { return false }
	for _, prefix := range p.allowed { if prefix.Contains(addr) { return true } }
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() { return false }
	for _, prefix := range specialNetworks { if prefix.Contains(addr) { return false } }
	return true
}

func (p webhookPolicy) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil { return nil, err }
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		ips, err = net.DefaultResolver.LookupNetIP(ctx,"ip",host)
		if err != nil { return nil, err }
	}
	if len(ips)==0 { return nil, errors.New("webhook host has no addresses") }
	// Reject mixed public/private DNS answers too. Dial only validated IPs;
	// the transport still uses the original host for TLS and HTTP.
	for _, ip := range ips { if !p.permits(ip) { return nil, errors.New("webhook destination blocked by address policy") } }
	dialer := net.Dialer{Timeout: 10*time.Second, KeepAlive: 30*time.Second}
	for _, ip := range ips {
		var conn net.Conn
		conn, err = dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(),port))
		if err==nil { return conn,nil }
	}
	return nil,err
}

func validWebhookURL(req *http.Request) bool {
	return req.URL != nil && (req.URL.Scheme=="http" || req.URL.Scheme=="https") &&
		req.URL.Hostname()!="" && req.URL.User==nil && req.URL.Opaque==""
}

type webhookTransport struct { base *http.Transport }
func (t webhookTransport) RoundTrip(req *http.Request) (*http.Response,error) {
	if !validWebhookURL(req) { return nil,errors.New("webhook requires HTTP(S) URL without userinfo") }
	return t.base.RoundTrip(req)
}

func (p webhookPolicy) client(verify bool) *http.Client {
	t := &http.Transport{
		Proxy: nil, DialContext: p.dial,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !verify, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 10*time.Second, ResponseHeaderTimeout: 15*time.Second,
		IdleConnTimeout: 60*time.Second, MaxIdleConns: 32, MaxIdleConnsPerHost: 4,
		MaxConnsPerHost: 8, MaxResponseHeaderBytes: 64<<10,
	}
	return &http.Client{Transport: webhookTransport{t}, Timeout: 30*time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via)>=5 || !validWebhookURL(req) || len(via)==0 ||
				!strings.EqualFold(req.URL.Host,via[0].URL.Host) || req.URL.Scheme!=via[0].URL.Scheme {
				return errors.New("webhook cross-origin redirect blocked")
			}
			return nil
		}}
}

func ConfigureWebhookPolicy(cidrs []string) error {
	p := webhookPolicy{}
	for _, raw := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || prefix.Addr().Is4In6() { return fmt.Errorf("invalid webhookallowedcidrs entry %q", raw) }
		p.allowed = append(p.allowed,prefix.Masked())
	}
	activeWebhookClients.Store(&webhookClients{p.client(true),p.client(false)})
	return nil
}

func WebhookClient(verify bool) *http.Client {
	c := activeWebhookClients.Load()
	if c==nil {
		p:=webhookPolicy{}
		fallback:=&webhookClients{p.client(true),p.client(false)}
		activeWebhookClients.CompareAndSwap(nil,fallback)
		c=activeWebhookClients.Load()
	}
	if verify { return c.verified }
	return c.unverified
}

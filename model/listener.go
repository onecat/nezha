package model

import (
 "errors"
 "net"
 "net/netip"
 "strconv"
)

// Empty hosts preserve legacy wildcard binding. Explicit values must be
// numeric IPv4/IPv6 addresses (no hostname, port, brackets or scope zone).
func validateBindHost(host string)error{
 if host==""{return nil}
 addr,err:=netip.ParseAddr(host)
 if err!=nil||addr.Zone()!=""{return errors.New("bind host must be an IPv4 or IPv6 literal")}
 return nil
}
func(c *Config)ValidateListeners()error{
 if err:=validateBindHost(c.HTTPBindHost);err!=nil{return errors.New("invalid httpbindhost: use an IPv4 or IPv6 literal")}
 if err:=validateBindHost(c.GRPCBindHost);err!=nil{return errors.New("invalid grpcbindhost: use an IPv4 or IPv6 literal")}
 if c.HTTPPort>65535||c.GRPCPort>65535{return errors.New("invalid HTTP or gRPC port")}
 return nil
}
func(c *Config)HTTPListenAddress(port uint)string{
 return net.JoinHostPort(c.HTTPBindHost,strconv.FormatUint(uint64(port),10))
}
// Managed upgrades require a trusted loopback TLS-terminating proxy.
// Never silently relax that boundary because a wildcard was configured.
func(c *Config)GRPCListenAddress(port uint,managed bool)(string,error){
 host:=c.GRPCBindHost
 if err:=validateBindHost(host);err!=nil{return "",err}
 if managed{
  if host==""{host="127.0.0.1"}
  ip,err:=netip.ParseAddr(host)
  if err!=nil||!ip.IsLoopback(){return "",errors.New("managed upgrades require grpcbindhost to be a loopback address")}
 }
 return net.JoinHostPort(host,strconv.FormatUint(uint64(port),10)),nil
}

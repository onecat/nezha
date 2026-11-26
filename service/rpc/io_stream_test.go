package rpc

import (
	"net"
	"sync"
	"testing"
	"time"
)

func TestStreamOwnership(t *testing.T) {
	s := NewNezhaHandler()
	if err := s.CreateStream("id", 10, 20, StreamTerminal); err != nil { t.Fatal(err) }
	defer s.CloseStream("id")
	if s.CanConnectUser("id", 11, StreamTerminal) || s.CanConnectUser("id", 10, StreamFile) || s.CanConnectUser("id", 0, StreamNAT) {
		t.Fatal("wrong owner or type accepted")
	}
	if !s.CanConnectUser("id", 10, StreamTerminal) { t.Fatal("owner rejected") }
	u, up := net.Pipe(); defer u.Close(); defer up.Close()
	a, ap := net.Pipe(); defer a.Close(); defer ap.Close()
	if s.AgentConnected("id", 21, a) == nil { t.Fatal("wrong agent accepted") }
	if err := s.AgentConnected("id", 20, a); err != nil { t.Fatal(err) }
	if s.AgentConnected("id", 20, a) == nil { t.Fatal("duplicate agent accepted") }
	if err := s.UserConnected("id", 10, StreamTerminal, u); err != nil { t.Fatal(err) }
	if s.UserConnected("id", 10, StreamTerminal, u) == nil { t.Fatal("duplicate user accepted") }
	done := make(chan error, 1)
	go func(){ done <- s.StartStream("id", time.Second) }()
	go func(){ _, _ = ap.Write([]byte("ok")) }()
	buf := make([]byte, 2)
	_ = up.SetReadDeadline(time.Now().Add(time.Second))
	n, err := up.Read(buf)
	if err != nil || n != 2 || string(buf) != "ok" { t.Fatalf("forward failed: %q %v", buf, err) }
	_ = up.Close()
	select { case <-done: case <-time.After(time.Second): t.Fatal("copy did not exit") }
}

func TestStreamConcurrentConnect(t *testing.T) {
	s := NewNezhaHandler()
	if err := s.CreateStream("id", 1, 2, StreamFile); err != nil { t.Fatal(err) }
	defer s.CloseStream("id")
	var wg sync.WaitGroup
	results := make(chan bool, 16)
	for i:=0; i<cap(results); i++ {
		wg.Add(1)
		go func(){ defer wg.Done(); a,b:=net.Pipe(); defer b.Close()
			ok:=s.UserConnected("id",1,StreamFile,a)==nil
			if !ok { _=a.Close() }; results<-ok
		}()
	}
	wg.Wait(); close(results)
	count:=0; for ok:=range results { if ok { count++ } }
	if count!=1 { t.Fatalf("accepted %d",count) }
}

func TestStreamLimitAndTimeout(t *testing.T) {
	s:=NewNezhaHandler()
	for i:=0;i<maxStreams;i++ {
		id:=string(rune(i+1))
		if err:=s.CreateStream(id,0,2,StreamNAT); err!=nil { t.Fatal(err) }
		defer s.CloseStream(id)
	}
	if s.CreateStream("overflow",0,2,StreamNAT)==nil { t.Fatal("capacity exceeded") }
	if s.StartStream(string(rune(1)),time.Millisecond)==nil { t.Fatal("missing peers accepted") }
}

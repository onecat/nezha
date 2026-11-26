package rpc

import (
	"errors"
	"io"
	"sync"
	"time"
)

type StreamKind uint8

const (
	StreamTerminal StreamKind = iota + 1
	StreamFile
	StreamNAT
	maxStreams = 256
	pendingStreamAge = 30 * time.Second
)

type ioStreamContext struct {
	ownerID, agentID uint64
	kind StreamKind
	userIo, agentIo io.ReadWriteCloser
	userReady, agentReady, done chan struct{}
	timer *time.Timer
	started bool
}

var bufPool = sync.Pool{New: func() any { b := make([]byte, 64*1024); return &b }}
var errStream = errors.New("stream unavailable")

func (s *NezhaHandler) CreateStream(id string, ownerID, agentID uint64, kind StreamKind) error {
	if id == "" || agentID == 0 || (kind != StreamTerminal && kind != StreamFile && kind != StreamNAT) ||
		(kind == StreamNAT && ownerID != 0) || (kind != StreamNAT && ownerID == 0) {
		return errStream
	}
	s.ioStreamMutex.Lock()
	defer s.ioStreamMutex.Unlock()
	if len(s.ioStreams) >= maxStreams || s.ioStreams[id] != nil {
		return errStream
	}
	ctx := &ioStreamContext{ownerID: ownerID, agentID: agentID, kind: kind,
		userReady: make(chan struct{}), agentReady: make(chan struct{}), done: make(chan struct{})}
	s.ioStreams[id] = ctx
	ctx.timer = time.AfterFunc(pendingStreamAge, func() { s.closeStream(id, ctx, true) })
	return nil
}

func (s *NezhaHandler) CanConnectUser(id string, ownerID uint64, kind StreamKind) bool {
	s.ioStreamMutex.RLock()
	defer s.ioStreamMutex.RUnlock()
	ctx := s.ioStreams[id]
	return ctx != nil && ctx.ownerID == ownerID && ctx.kind == kind && ctx.userIo == nil
}

func (s *NezhaHandler) closeStream(id string, expected *ioStreamContext, pendingOnly bool) {
	s.ioStreamMutex.Lock()
	ctx := s.ioStreams[id]
	if ctx == nil || (expected != nil && ctx != expected) || (pendingOnly && ctx.started) {
		s.ioStreamMutex.Unlock()
		return
	}
	delete(s.ioStreams, id)
	ctx.timer.Stop()
	close(ctx.done)
	user, agent := ctx.userIo, ctx.agentIo
	s.ioStreamMutex.Unlock()
	// Never close a connection while holding the global stream lock.
	if user != nil { _ = user.Close() }
	if agent != nil { _ = agent.Close() }
}

func (s *NezhaHandler) CloseStream(id string) error {
	s.closeStream(id, nil, false)
	return nil
}

func (s *NezhaHandler) UserConnected(id string, ownerID uint64, kind StreamKind, conn io.ReadWriteCloser) error {
	s.ioStreamMutex.Lock()
	defer s.ioStreamMutex.Unlock()
	ctx := s.ioStreams[id]
	if ctx == nil || ctx.ownerID != ownerID || ctx.kind != kind || ctx.userIo != nil || conn == nil {
		return errStream
	}
	ctx.userIo = conn
	close(ctx.userReady)
	return nil
}

func (s *NezhaHandler) AgentConnected(id string, agentID uint64, conn io.ReadWriteCloser) error {
	s.ioStreamMutex.Lock()
	defer s.ioStreamMutex.Unlock()
	ctx := s.ioStreams[id]
	if ctx == nil || ctx.agentID != agentID || ctx.agentIo != nil || conn == nil {
		return errStream
	}
	ctx.agentIo = conn
	close(ctx.agentReady)
	return nil
}

func (s *NezhaHandler) StartStream(id string, timeout time.Duration) error {
	s.ioStreamMutex.RLock()
	ctx := s.ioStreams[id]
	s.ioStreamMutex.RUnlock()
	if ctx == nil { return errStream }
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for _, ready := range []chan struct{}{ctx.userReady, ctx.agentReady} {
		select {
		case <-ready:
		case <-ctx.done: return errStream
		case <-timer.C: return errors.New("stream connection timeout")
		}
	}
	s.ioStreamMutex.Lock()
	if s.ioStreams[id] != ctx || ctx.started {
		s.ioStreamMutex.Unlock()
		return errStream
	}
	ctx.started = true
	ctx.timer.Stop()
	user, agent := ctx.userIo, ctx.agentIo
	s.ioStreamMutex.Unlock()
	defer s.closeStream(id, ctx, false)
	result := make(chan error, 2)
	copyStream := func(dst io.Writer, src io.Reader) {
		buf := bufPool.Get().(*[]byte)
		defer bufPool.Put(buf)
		_, err := io.CopyBuffer(dst, src, *buf)
		result <- err
	}
	go copyStream(user, agent)
	go copyStream(agent, user)
	select {
	case err := <-result: return err
	case <-ctx.done: return errStream
	}
}

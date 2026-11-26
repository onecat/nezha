package grpcx

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"

	"github.com/naiba/nezha/proto"
)

var _ io.ReadWriteCloser = &IOStreamWrapper{}

type IOStream interface {
	Recv() (*proto.IOStreamData, error)
	Send(*proto.IOStreamData) error
	Context() context.Context
}

type IOStreamWrapper struct {
	IOStream
	dataBuf []byte
	legacyNAT bool
	pending *proto.IOStreamData
	pendingErr error
	closed  *atomic.Bool
	closeCh chan struct{}
}

func NewIOStreamWrapper(stream IOStream) *IOStreamWrapper {
	return newIOStreamWrapper(stream, false)
}

// NewLegacyNATStreamWrapper handles the V0 Agent convention of sending a
// separate EOF text message immediately before closing its send stream.
func NewLegacyNATStreamWrapper(stream IOStream) *IOStreamWrapper {
	return newIOStreamWrapper(stream, true)
}

func newIOStreamWrapper(stream IOStream, legacyNAT bool) *IOStreamWrapper {
	return &IOStreamWrapper{
		IOStream: stream,
		legacyNAT: legacyNAT,
		closeCh:  make(chan struct{}),
		closed:   new(atomic.Bool),
	}
}

func (iw *IOStreamWrapper) Read(p []byte) (n int, err error) {
	if len(iw.dataBuf) > 0 {
		n := copy(p, iw.dataBuf)
		iw.dataBuf = iw.dataBuf[n:]
		return n, nil
	}
	var data *proto.IOStreamData
	if data, err = iw.recvData(); err != nil {
		return 0, err
	}
	n = copy(p, data.Data)
	if n < len(data.Data) {
		iw.dataBuf = data.Data[n:]
	}
	return n, nil
}

// recvData preserves message boundaries while distinguishing the legacy NAT
// EOF sentinel from actual payload bytes. Never strip substrings or markers
// in terminal/file streams. A non-final standalone EOF frame is real data.
func (iw *IOStreamWrapper) recvData() (*proto.IOStreamData, error) {
	if iw.pendingErr != nil {
		err := iw.pendingErr
		iw.pendingErr = nil
		return nil, err
	}
	data := iw.pending
	iw.pending = nil
	if data == nil {
		var err error
		data, err = iw.Recv()
		if err != nil { return nil, err }
	}
	if data == nil { return nil, io.ErrUnexpectedEOF }
	if !iw.legacyNAT || !bytes.Equal(data.Data, []byte("EOF")) { return data, nil }
	next, err := iw.Recv()
	if err == io.EOF { return nil, io.EOF }
	// An error other than graceful EOF does not prove a terminal sentinel.
	// Preserve the possible payload, then surface the actual error.
	iw.pending, iw.pendingErr = next, err
	return data, nil
}

func (iw *IOStreamWrapper) Write(p []byte) (n int, err error) {
	err = iw.Send(&proto.IOStreamData{Data: p})
	return len(p), err
}

func (iw *IOStreamWrapper) Close() error {
	if iw.closed.CompareAndSwap(false, true) {
		close(iw.closeCh)
	}
	return nil
}

func (iw *IOStreamWrapper) Wait() {
	select {
	case <-iw.closeCh:
	case <-iw.Context().Done():
	}
}

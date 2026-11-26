package websocketx

import (
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var _ io.ReadWriteCloser = &Conn{}

type Conn struct {
	*websocket.Conn
	writeLock *sync.Mutex
	dataBuf   []byte
}

func NewConn(conn *websocket.Conn) *Conn {
	conn.SetReadLimit(4 << 20) // file upload uses 1 MiB chunks
	_ = conn.SetReadDeadline(time.Now().Add(90*time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(90*time.Second)) })
	return &Conn{Conn: conn, writeLock: new(sync.Mutex)}
}

func (conn *Conn) Write(data []byte) (int, error) {
	conn.writeLock.Lock()
	defer conn.writeLock.Unlock()
	_ = conn.Conn.SetWriteDeadline(time.Now().Add(15*time.Second))
	if err := conn.Conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (conn *Conn) WriteMessage(messageType int, data []byte) error {
	conn.writeLock.Lock()
	defer conn.writeLock.Unlock()
	_ = conn.Conn.SetWriteDeadline(time.Now().Add(15*time.Second))
	return conn.Conn.WriteMessage(messageType, data)
}

func (conn *Conn) Read(data []byte) (int, error) {
	if len(conn.dataBuf) > 0 {
		n := copy(data, conn.dataBuf)
		conn.dataBuf = conn.dataBuf[n:]
		return n, nil
	}
	_ = conn.Conn.SetReadDeadline(time.Now().Add(90*time.Second))
	mType, innerData, err := conn.Conn.ReadMessage()
	if err != nil {
		return 0, err
	}
	// 将文本消息转换为命令输入
	if mType == websocket.TextMessage {
		innerData = append([]byte{0}, innerData...)
	}
	n := copy(data, innerData)
	if n < len(innerData) {
		conn.dataBuf = innerData[n:]
	}
	return n, nil
}

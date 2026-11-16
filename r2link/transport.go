package r2link

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"

	"github.com/coder/websocket"
)

// Conn 只抽象完整消息，数据源和客户端都使用相同的编码器。
// 读取和写入可各有一个协程，Close 会中断两者。
type Conn struct {
	tcp   net.Conn
	ws    *websocket.Conn
	limit int64
}

// TCP 借用已连接的 socket，预算同时限制一次在途消息的大小。
func TCP(conn net.Conn, limit int64) *Conn { return &Conn{tcp: conn, limit: limit} }

// WebSocket 使用库处理分片，R2Link 只接收完整的二进制消息。
func WebSocket(conn *websocket.Conn, limit int64) *Conn {
	conn.SetReadLimit(limit)
	return &Conn{ws: conn, limit: limit}
}

// Dial 让 Go 服务主动连接来源，context 控制拨号，调用方拥有连接的关闭责任。
func Dial(ctx context.Context, address string, limit int64) (*Conn, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	switch parsed.Scheme {
	case "tcp":
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", parsed.Host)
		if err != nil {
			return nil, err
		}
		return TCP(conn, limit), nil
	case "ws", "wss":
		conn, _, err := websocket.Dial(ctx, address, nil)
		if err != nil {
			return nil, err
		}
		return WebSocket(conn, limit), nil
	default:
		return nil, fmt.Errorf("连接地址使用 tcp://、ws:// 或 wss://")
	}
}

// Read 先恢复传输的消息边界，再交给共用解码器；取消会关闭传输，避免接着读取半条消息。
func (c *Conn) Read(ctx context.Context) (Message, error) {
	var data []byte
	if c.ws != nil {
		kind, body, err := c.ws.Read(ctx)
		if err != nil {
			return Message{}, err
		}
		if kind != websocket.MessageBinary {
			return Message{}, fmt.Errorf("R2Link 使用二进制 WebSocket 消息")
		}
		data = body
	} else {
		stop := context.AfterFunc(ctx, func() { c.tcp.Close() })
		defer stop()
		var prefix [4]byte
		if _, err := io.ReadFull(c.tcp, prefix[:]); err != nil {
			return Message{}, err
		}
		n := int64(binary.LittleEndian.Uint32(prefix[:]))
		if n > c.limit {
			return Message{}, fmt.Errorf("连接消息超出内存预算")
		}
		data = make([]byte, int(n))
		if _, err := io.ReadFull(c.tcp, data); err != nil {
			return Message{}, err
		}
	}
	return Decode(data)
}

// Write 的 TCP 长度前缀不进入共用消息体，WS 保持一条消息一个消息体。
func (c *Conn) Write(ctx context.Context, message Message) error {
	data, err := Encode(message)
	if err != nil {
		return err
	}
	if c.ws != nil {
		return c.ws.Write(ctx, websocket.MessageBinary, data)
	}
	stop := context.AfterFunc(ctx, func() { c.tcp.Close() })
	defer stop()
	framed := binary.LittleEndian.AppendUint32(nil, uint32(len(data)))
	framed = append(framed, data...)
	for len(framed) > 0 {
		n, err := c.tcp.Write(framed)
		if err != nil {
			return err
		}
		framed = framed[n:]
	}
	return nil
}

// Close 归还传输并唤醒正在等待读取的协程。
func (c *Conn) Close() error {
	if c.ws != nil {
		return c.ws.CloseNow()
	}
	return c.tcp.Close()
}

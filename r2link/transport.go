package r2link

import (
	"context"
	"fmt"
	"net/url"

	"github.com/coder/websocket"
)

// Conn 只抽象完整消息，数据源和客户端都使用相同的编码器。
// R2Link 只走 WebSocket，浏览器可以直接连接；一条 Binary Message 就是一条消息。
// 读取和写入可各有一个协程，Close 会中断两者。
type Conn struct {
	ws *websocket.Conn
}

// WebSocket 使用库处理分片，limit 限制一条在途消息的大小。
func WebSocket(conn *websocket.Conn, limit int64) *Conn {
	conn.SetReadLimit(limit)
	return &Conn{ws: conn}
}

// Dial 主动连接 ws:// 或 wss:// 地址，context 控制拨号，调用方拥有连接的关闭责任。
func Dial(ctx context.Context, address string, limit int64) (*Conn, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return nil, fmt.Errorf("连接地址使用 ws:// 或 wss://")
	}
	conn, _, err := websocket.Dial(ctx, address, nil)
	if err != nil {
		return nil, err
	}
	return WebSocket(conn, limit), nil
}

// Read 读取一条完整消息并解码。
func (c *Conn) Read(ctx context.Context) (Message, error) {
	kind, body, err := c.ws.Read(ctx)
	if err != nil {
		return Message{}, err
	}
	if kind != websocket.MessageBinary {
		return Message{}, fmt.Errorf("R2Link 使用二进制 WebSocket 消息")
	}
	return Decode(body)
}

// Write 把一条消息编码成一个 Binary Message。
func (c *Conn) Write(ctx context.Context, message Message) error {
	data, err := Encode(message)
	if err != nil {
		return err
	}
	return c.ws.Write(ctx, websocket.MessageBinary, data)
}

// Close 归还传输并唤醒正在等待读取的协程。
func (c *Conn) Close() error {
	return c.ws.CloseNow()
}

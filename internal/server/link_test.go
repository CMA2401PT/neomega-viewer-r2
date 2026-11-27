package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"neomega-r2link"
)

// linkClient 以浏览器的身份连接 /r2link 并完成握手。
type linkClient struct {
	t    *testing.T
	conn *r2link.Conn
}

// dialWorld 连接指定世界；世界不是当前会话时返回错误。
func dialWorld(t *testing.T, server *httptest.Server, id uint64) (*linkClient, r2link.Welcome, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	address := fmt.Sprintf("ws%s/r2link?world=%d", strings.TrimPrefix(server.URL, "http"), id)
	conn, err := r2link.Dial(ctx, address)
	if err != nil {
		return nil, r2link.Welcome{}, err
	}
	t.Cleanup(func() { conn.CloseNow() })
	client := &linkClient{t: t, conn: conn}
	client.send(r2link.Hello{Type: "hello", Protocol: "r2link", Version: r2link.Version})
	var welcome r2link.Welcome
	client.receive(&welcome)
	if welcome.Type != "welcome" {
		t.Fatalf("握手回复 %+v", welcome)
	}
	return client, welcome, nil
}

func (c *linkClient) send(v any) {
	c.t.Helper()
	if err := c.conn.Send(context.Background(), v, nil); err != nil {
		c.t.Fatal(err)
	}
}

// next 读取一条消息，超时或断开时返回错误。
func (c *linkClient) next() (r2link.Frame, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.conn.Receive(ctx)
}

func (c *linkClient) receive(v any) r2link.Frame {
	c.t.Helper()
	frame, err := c.next()
	if err != nil {
		c.t.Fatal(err)
	}
	if frame.Type == "failed" || frame.Type == "error" {
		c.t.Fatalf("%s：%s", frame.Type, frame.Header)
	}
	if v != nil {
		if err := frame.Decode(v); err != nil {
			c.t.Fatal(err)
		}
	}
	return frame
}

// want 发送一次订阅变化。
func (c *linkClient) want(regions, removed []r2link.RegionPos, objects []r2link.BlockEntityWant) {
	c.t.Helper()
	var want r2link.Want
	want.Type = "want"
	want.Regions.Add, want.Regions.Remove, want.BlockEntities.Add = regions, removed, objects
	c.send(want)
}

// chunks 请求占用表。
func (c *linkClient) chunks() []byte {
	c.t.Helper()
	c.send(r2link.Chunks{Type: "chunks"})
	var reply r2link.Chunks
	frame := c.receive(&reply)
	if reply.Type != "chunks" || reply.Message != "" {
		c.t.Fatalf("占用表回复 %+v", reply)
	}
	return frame.Tail
}

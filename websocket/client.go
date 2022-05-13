package websocket

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/klintcheng/kim/logger"
	"kim"
)

type ClientOptions struct {
	Heartbeat time.Duration
	ReadWait  time.Duration
	WriteWait time.Duration
}

type Client struct {
	sync.Mutex
	kim.Dialer
	once    sync.Once
	id      string
	name    string
	conn    net.Conn
	state   int32
	options ClientOptions
}

func NewClient(id, name string, opts ClientOptions) kim.Client {
	if opts.WriteWait == 0 {
		opts.WriteWait = kim.DefaultWriteWait
	}
	if opts.ReadWait == 0 {
		opts.ReadWait = kim.DefaultReadWait
	}
	cli := &Client{
		id:      id,
		name:    name,
		options: opts,
	}
	return cli
}

func (c *Client) Connect(addr string) error {
	_, err := url.Parse(addr)
	if err != nil {
		return err
	}
	if !atomic.CompareAndSwapInt32(&c.state, 0, 1) {
		return fmt.Errorf("client has connected")
	}
	conn, err := c.Dialer.DialAndHandshake(kim.DialerContext{
		Id:      c.id,
		Name:    c.name,
		Address: addr,
		Timeout: kim.DefaultLoginWait,
	})
	if err != nil {
		atomic.CompareAndSwapInt32(&c.state, 1, 0)
		return err
	}
	if conn == nil {
		return fmt.Errorf("conn is nil")
	}
	c.conn = conn

	if c.options.Heartbeat > 0 {
		go func() {
			err := c.heartbeatLoop(conn)
			if err != nil {
				logger.Error("heartbeatLoop stopped", err)
			}
		}()
	}
	return nil
}

func (c *Client) heartbeatLoop(conn net.Conn) error {
	tick := time.NewTicker(c.options.Heartbeat)
	for range tick.C {
		if err := c.ping(conn); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) ping(conn net.Conn) error {
	c.Lock()
	defer c.Unlock()
	err := conn.SetWriteDeadline(time.Now().Add(c.options.WriteWait))
	if err != nil {
		return err
	}
	logger.Tracef("%s send ping to server", c.id)
	return wsutil.WriteClientMessage(conn, ws.OpPing, nil)
}

func (c *Client) Read() (kim.Frame, error) {
	if c.conn == nil {
		return nil, errors.New("connection is nil")
	}
	if c.options.ReadWait > 0 {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.options.ReadWait))
	}
	frame, err := ws.ReadFrame(c.conn)
	if err != nil {
		return nil, err
	}
	if frame.Header.OpCode == ws.OpClose {
		return nil, errors.New("remote side close the channel")
	}
	return &FrameImpl{
		raw: frame,
	}, nil
}

func (c *Client) Send(payload []byte) error {
	if atomic.LoadInt32(&c.state) == 0 {
		return fmt.Errorf("connection is nil")
	}
	c.Lock()
	defer c.Unlock()
	err := c.conn.SetWriteDeadline(time.Now().Add(c.options.WriteWait))
	if err != nil {
		return err
	}

	return wsutil.WriteClientMessage(c.conn, ws.OpBinary, payload)
}

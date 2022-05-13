package kim

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/klintcheng/kim/logger"
	"kim/websocket"
)

type OpCode int

const (
	OpContinuation OpCode = 0x0
	OpText         OpCode = 0x1
	OpBinary       OpCode = 0x2
	OpClose        OpCode = 0x8
	OpPing         OpCode = 0x9
	OpPong         OpCode = 0xa
)

// Server
type Server interface {
	// SetAcceptor 设置管理握手逻辑
	SetAcceptor(Acceptor)
	// SetMessageListener 设置消息监听器
	SetMessageListener(MessageListener)
	// SetStateListener 设置连接状态管理
	SetStateListener(StateListener)
	// SetReadWait 处理心跳间隔
	SetReadWait(duration time.Duration)
	// SetChannelMap 设置连接管理
	SetChannelMap(ChannelMap)

	// Start starting server and listening
	Start() error
	Push(string, []byte) error
	Shutdown(ctx context.Context) error
}

// Acceptor 处理握手逻辑
type Acceptor interface {
	Accept(Conn, time.Duration) (string, error)
}

// StateListener 处理连接状态事件
type StateListener interface {
	Disconnect(string) error
}

// ChannelMap 管理连接
type ChannelMap interface {
	Add(channel Channel)
	Remove(id string)
	Get(id string) (Channel, bool)
	All() []Channel
}

// MessageListener 消息监听
type MessageListener interface {
	Receive(Agent, []byte)
}

// Agent ??
type Agent interface {
	ID() string
	Push([]byte) error
}

// Frame 处理底层封包拆包逻辑
type Frame interface {
	SetOpCode(OpCode)
	GetOpCode() OpCode
	SetPayload([]byte)
	GetPayload() []byte
}

// Conn connection
type Conn interface {
	net.Conn
	ReadFrame() (*websocket.FrameImpl, error)
	WriteFrame(OpCode, []byte) error
	Flush() error
}

// Client is interface of client side
type Client interface {
	ID() string
	Name() string
	// Connect 向服务器发起连接
	Connect(string) error
	// SetDialer set a dialer for handshaking
	SetDialer(Dialer)
	// Send 发送消息
	Send([]byte) error
	// Read reading message
	Read() (websocket.FrameImpl, error)
	// Close closing connection
	Close()
}

type Dialer interface {
	DialAndHandshake(DialerContext) (net.Conn, error)
}

type DialerContext struct {
	Id      string
	Name    string
	Address string
	Timeout time.Duration
}

type ServerDemo struct {
}

func (s *ServerDemo) Start(id, protocol, addr string) {
	var srv Server
	service := &DefaultService{
		Id:       id,
		Protocol: protocol,
	}
	if protocol == "ws" {
		srv = websocketNewServer(addr, service)
	} else if protocol == "tcp" {
		srv = tcpNewServer(addr, service)
	}

	handler := &ServerHandler{}

	srv.SetReadWait(time.Minute)
	srv.SetAcceptor(handler)
	srv.SetMessageListener(handler)
	srv.SetStateListener(handler)

	err := srv.Start()
	if err != nil {
		panic(err)
	}
}

type ServerHandler struct {
}

func (h *ServerHandler) Accept(conn Conn, timeout time.Duration) (string, error) {
	// 1. read message from client
	frame, err := conn.ReadFrame()
	if err != nil {
		return "", err
	}

	// 2. parse payload
	userID := string(frame.GetPayload())

	// 3. auth
	if userID == "" {
		return "", errors.New("user id is invalid")
	}

	return userID, nil
}

func (h *ServerHandler) Receive(ag Agent, payload []byte) {
	ack := string(payload) + " from server"
	_ = ag.Push([]byte(ack))
}

func (h *ServerHandler) Disconnect(id string) error {
	logger.Warnf("disconnect %s", id)
	return nil
}

type ClientDemo struct {
}

func (c *ClientDemo) Start(userID, protocol, addr string) {
	var cli Client

	if protocol == "ws" {
		cli = websocketNewClient(userID, "client", websocketClientOptions{})
		cli.SetDialer(&WebsocketDialer{})
	} else if protocol == "tcp" {
		cli = tcpNewClient(userID, "client", tcpClientOptions{})
		cli.SetDialer(&TcpDialer{})
	}

	err := cli.Connect(addr)
	if err != nil {
		logger.Error(err)
	}

	count := 10
	go func() {
		for i := 0; i < count; i++ {
			err := cli.Send([]byte("hello"))
			if err != nil {
				logger.Error(err)
				return
			}
			time.Sleep(time.Second)
		}
	}()

	recv := 0
	for {
		frame, err := cli.Read()
		if err != nil {
			logger.Info(err)
			break
		}
		if frame.GetOpCode() != OpBinary {
			continue
		}
		recv++
		logger.Warnf("%s receive message [%s]", cli.ID(), frame.GetPayload())
		if recv == count {
			break
		}
	}

	cli.Close()
}

type ClientHandler struct {
}

func (h *ClientHandler) Receive(ag Agent, payload []byte) {
	logger.Warnf("%s receive message [%s]", ag.ID(), string(payload))
}

func (h *ClientHandler) Disconnect(id string) error {
	logger.Warnf("disconnect %s", id)
	return nil
}

type WebsocketDialer struct {
}

func (d *WebsocketDialer) DialAndHandshake(ctx DialerContext) (net.Conn, error) {
	conn, _, _, err := ws.Dial(context.TODO(), ctx.Address)
	if err != nil {
		return nil, err
	}

	err = wsutil.WriteClientBinary(conn, []byte(ctx.Id))
	if err != nil {
		return nil, err
	}

	return conn, nil
}

type TcpDialer struct {
}

func (d *TcpDialer) DialAndHandshake(ctx DialerContext) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", ctx.Address, ctx.Timeout)
	if err != nil {
		return nil, err
	}

	err = WriteFrame(conn, OpBinary, []byte(ctx.Id))
	if err != nil {
		return nil, err
	}

	return conn, nil
}

func WriteFrame(w io.Writer, code OpCode, payload []byte) error {
	if err := WriteUint8(w, uint8(code)); err != nil {
		return err
	}
	if err := WriteBytes(w, payload); err != nil {
		return err
	}
	return nil
}

func WriteBytes(w io.Writer, buf []byte) error {
	bufLen := len(buf)

	if err := WriteUint32(w, uint32(bufLen)); err != nil {
		return err
	}
	if _, err := w.Write(buf); err != nil {
		return err
	}
	return nil
}

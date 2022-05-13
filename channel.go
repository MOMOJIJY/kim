package kim

import (
	"errors"
	"sync"
	"time"

	"github.com/klintcheng/kim/logger"
)

type Channel interface {
	Conn
	Agent
	// Close 重写net.Conn的Close方法
	Close() error
	Readloop(lst MessageListener) error
	SetWriteWait(duration time.Duration)
	SetReadWait(duration time.Duration)
}

type ChannelImpl struct {
	sync.Mutex
	id string
	Conn
	writeChan chan []byte
	once      sync.Once
	writeWait time.Duration
	readWait  time.Duration
	closed    *Event
}

func NewChannel(id string, conn Conn) Channel {
	log := logger.WithFields(logger.Fields{
		"module": "tcp_channel",
		"id":     id,
	})
	ch := &ChannelImpl{
		id:        id,
		Conn:      conn,
		writeChan: make(chan []byte, 5),
		closed:    NewEvent(),
		writeWait: 10 * time.Second,
	}
	return ch
}

func (ch *ChannelImpl) writeloop() error {
	for {
		select {
		case payload := <-ch.writeChan:
			err := ch.WriteFrame(OpBinary, payload)
			if err != nil {
				return err
			}
			// 批量写
			chanLen := len(ch.writeChan)
			for i := 0; i < chanLen; i++ {
				payload = <-ch.writeChan
				err := ch.WriteFrame(OpBinary, payload)
				if err != nil {
					return err
				}
			}
			err = ch.Conn.Flush()
			if err != nil {
				return err
			}
		case <-ch.closed.Done():
			return nil
		}
	}
}

func (ch *ChannelImpl) ID() string { return ch.id }

func (ch *ChannelImpl) Push(payload []byte) error {
	if ch.closed.HasFired() {
		return errors.New("channel has closed")
	}
	// 异步写
	ch.writeChan <- payload
	return nil
}

// overwrite Conn
func (ch *ChannelImpl) WriteFrame(code OpCode, payload []byte) error {
	_ = ch.Conn.SetWriteDeadline(time.Now().Add(ch.writeWait))
	return ch.Conn.WriteFrame(code, payload)
}

func (ch *ChannelImpl) Readloop(lst MessageListener) error {
	ch.Lock()
	defer ch.Unlock()
	log := logger.WithFields(logger.Fields{
		"struct": "ChannelImpl",
		"func":   "Readloop",
		"id":     ch.id,
	})

	for {
		_ = ch.SetReadDeadline(time.Now().Add(ch.readWait))

		frame, err := ch.ReadFrame()
		if err != nil {
			return err
		}

		payload := frame.GetPayload()
		if len(payload) == 0 {
			continue
		}
		go lst.Receive(ch, payload)
	}
}

func (ch *ChannelImpl) SetWriteWait(duration time.Duration) {

}

func (ch *ChannelImpl) SetReadWait(duration time.Duration) {

}

func (ch *ChannelImpl) Close() error {
	return ch.Close()
}
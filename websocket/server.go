package websocket

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/klintcheng/kim/logger"
	"kim"
)

type ServerOptions struct {
	loginWait time.Duration
	readWait  time.Duration
	writeWait time.Duration
}

type Server struct {
	listen string
	naming.ServiceRegistration
	kim.ChannelMap
	kim.Acceptor
	kim.MessageListener
	kim.StateListener
	once    sync.Once
	options ServerOptions
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	log := logger.WithFields(logger.Fields{
		"module": "ws.server",
		"listen": s.listen,
		"id":     s.ServiceID(),
	})

	if s.Acceptor == nil {
		s.Acceptor = new(kim.ServerHandler)
	}
	if s.StateListener == nil {
		return fmt.Errorf("StateListener is nil")
	}
	if s.ChannelMap == nil {
		s.ChannelMap = kim.NewChannels(100)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// step 1
		rawConn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			resp(w, http.StatusBadRequest, err.Error())
		}

		// step 2
		conn := NewConn(rawConn)

		// step 3
		id, err := s.Accept(conn, s.options.loginWait)
		if err != nil {
			_ = conn.WriteFrame(kim.OpClose, []byte(err.Error()))
			conn.Close()
			return
		}
		if _, ok := s.Get(id); ok {
			log.Warnf("channel %s existed", id)
			_ = conn.WriteFrame(kim.OpClose, []byte("channelId is repeated"))
			conn.Close()
			return
		}

		// step 4
		channel := kim.NewChannel(id, conn)
		channel.SetWriteWait(s.options.writeWait)
		channel.SetReadWait(s.options.readWait)
		s.Add(channel)

		go func(ch kim.Channel) {
			// step 5
			err := ch.Readloop(s.MessageListener)
			if err != nil {
				log.Info(err)
			}
			// step 6
			s.Remove(ch.ID())
			err = s.Disconnect(ch.ID())
			if err != nil {
				log.Warn(err)
			}
			ch.Close()
		}(channel)
	})

	log.Infoln("started")
	return http.ListenAndServe(s.listen, mux)
}

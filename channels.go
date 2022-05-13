package kim

import "sync"

// ChannelMap 管理连接
type ChannelMap interface {
	Add(channel Channel)
	Remove(id string)
	Get(id string) (Channel, bool)
	All() []Channel
}

type ChannelMapImpl struct {
	sync.Map
}

func (m *ChannelMapImpl) Add() {

}
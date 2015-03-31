package mmap

import (
	"container/list"
	log "github.com/blackbeans/log4go"
	. "kiteq/store"
	"sync"
)

type KiteMMapStore struct {
	datalink *list.List                              //用于LRU
	idx      map[string] /*messageId*/ *list.Element //用于LRU
	lock     sync.RWMutex
	maxcap   int
	path     string
}

func NewKiteMMapStore(path string, initcap, maxcap int) *KiteMMapStore {
	return &KiteMMapStore{
		datalink: list.New(),
		idx:      make(map[string]*list.Element, initcap),
		maxcap:   maxcap,
		path:     path}
}

func (self *KiteMMapStore) Start() {}
func (self *KiteMMapStore) Stop()  {}

func (self *KiteMMapStore) AsyncUpdate(entity *MessageEntity) bool { return self.UpdateEntity(entity) }
func (self *KiteMMapStore) AsyncDelete(messageId string) bool      { return self.Delete(messageId) }
func (self *KiteMMapStore) AsyncCommit(messageId string) bool      { return self.Commit(messageId) }

func (self *KiteMMapStore) Query(messageId string) *MessageEntity {
	self.lock.RLock()
	defer self.lock.RUnlock()
	e, ok := self.idx[messageId]
	if !ok {
		return nil
	}
	//将当前节点放到最前面
	return e.Value.(*MessageEntity)
}

func (self *KiteMMapStore) Save(entity *MessageEntity) bool {
	self.lock.Lock()
	defer self.lock.Unlock()

	//没有空闲node，则判断当前的datalinke中是否达到容量上限
	cl := self.datalink.Len()
	if cl >= self.maxcap {
		log.Info("KiteMMapStore|SAVE|OVERFLOW|%d/%d\n", cl, self.maxcap)
		back := self.datalink.Back()
		delete(self.idx, back.Value.(*MessageEntity).MessageId)
		back.Value = entity
		self.datalink.MoveToFront(back)

	} else {
		front := self.datalink.PushFront(entity)
		self.idx[entity.MessageId] = front
	}

	return true
}
func (self *KiteMMapStore) Commit(messageId string) bool {
	self.lock.Lock()
	defer self.lock.Unlock()
	e, ok := self.idx[messageId]
	if !ok {
		return false
	}
	entity := e.Value.(*MessageEntity)
	entity.Commit = true
	return true
}
func (self *KiteMMapStore) Rollback(messageId string) bool {
	self.lock.Lock()
	defer self.lock.Unlock()
	e, ok := self.idx[messageId]
	if !ok {
		return true
	}
	delete(self.idx, messageId)
	self.datalink.Remove(e)
	return true
}
func (self *KiteMMapStore) UpdateEntity(entity *MessageEntity) bool {
	self.lock.Lock()
	defer self.lock.Unlock()
	v, ok := self.idx[entity.MessageId]
	if !ok {
		return true
	}

	e := v.Value.(*MessageEntity)
	e.DeliverCount = entity.DeliverCount
	e.NextDeliverTime = entity.NextDeliverTime
	e.SuccGroups = entity.SuccGroups
	e.FailGroups = entity.FailGroups
	return true
}
func (self *KiteMMapStore) Delete(messageId string) bool {
	return self.Rollback(messageId)

}

//根据kiteServer名称查询需要重投的消息 返回值为 是否还有更多、和本次返回的数据结果
func (self *KiteMMapStore) PageQueryEntity(hashKey string, kiteServer string, nextDeliveryTime int64, startIdx, limit int) (bool, []*MessageEntity) {
	pe := make([]*MessageEntity, 0, limit+1)
	self.lock.RLock()
	defer self.lock.RUnlock()
	for e := self.datalink.Back(); nil != e; e = e.Prev() {
		entity := e.Value.(*MessageEntity)
		if entity.NextDeliverTime <= nextDeliveryTime &&
			entity.DeliverCount < entity.Header.GetDeliverLimit() &&
			entity.ExpiredTime > entity.Header.GetExpiredTime() {
			pe = append(pe, entity)
			if len(pe) > limit {
				return true, pe[:limit]
			}
		}
	}
	return false, pe
}

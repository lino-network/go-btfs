package upload

import (
	"fmt"
	"github.com/TRON-US/go-btfs/core/commands/store/upload/ds"
	"github.com/ipfs/go-datastore"
	"github.com/looplab/fsm"
	"github.com/prometheus/common/log"
)

const (
	initStatus     = "init"
	contractStatus = "contract"

	toContractEvent = "to-contract"

	// btfs $peerId sessions $sessionId shards $shardHash
	shardKey       = "/btfs/%s/renter/sessions/%s/shards/%s/"
	shardStatusKey = shardKey + "status"
)

var (
	shardFsmEvents = fsm.Events{
		{Name: toContractEvent, Src: []string{initStatus}, Dst: contractStatus},
	}
)

type RenterShard struct {
	peerId string
	ssId   string
	hash   string
	fsm    *fsm.FSM
	ds     datastore.Datastore
}

func NewRenterShard(peerId string, ssId string, hash string, ds datastore.Datastore) *RenterShard {
	rs := &RenterShard{
		peerId: peerId,
		ssId:   ssId,
		hash:   hash,
		ds:     ds,
	}
	return rs
}

func (rs *RenterShard) init() {
	rs.fsm = fsm.NewFSM(initStatus, shardFsmEvents, fsm.Callbacks{
		"enter_state": rs.enterState,
	})
	ds.Save(rs.ds, fmt.Sprintf(shardKey, rs.peerId, rs.ssId, rs.hash), )
}

func (rs *RenterShard) contract() {
	rs.fsm.Event(toContractEvent)
}

func (rs *RenterShard) enterState(e *fsm.Event) {
	log.Debugf("shard: %s:%s enter statue: %s", rs.ssId, rs.hash, e.Dst)
}

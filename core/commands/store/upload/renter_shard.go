package upload

import (
	"context"
	"fmt"
	"github.com/TRON-US/go-btfs/core/commands/storage"
	"github.com/TRON-US/go-btfs/core/commands/store/upload/ds"
	renterpb "github.com/TRON-US/go-btfs/protos/renter"
	"github.com/ipfs/go-datastore"
	"github.com/looplab/fsm"
	cmap "github.com/orcaman/concurrent-map"
	"github.com/prometheus/common/log"
	"github.com/tron-us/protobuf/proto"
	"time"
)

const (
	rshInitStatus     = "init"
	rshContractStatus = "contract"
	rshErrorStatus    = "error"

	rshToContractEvent = "to-contract"

	// btfs $peerId sessions $sessionId shards $shardHash
	renterShardKey          = "/btfs/%s/renter/sessions/%s/shards/%s/"
	renterShardsInMemKey    = renterShardKey
	renterShardStatusKey    = renterShardKey + "status"
	renterShardContractsKey = renterShardKey + "contracts"
)

var (
	renterShardFsmEvents = fsm.Events{
		{Name: rshToContractEvent, Src: []string{rshInitStatus}, Dst: rshContractStatus},
	}
	renterShardsInMem = cmap.New()
)

type RenterShard struct {
	peerId string
	ssId   string
	hash   string
	fsm    *fsm.FSM
	ctx    context.Context
	ds     datastore.Datastore
}

func GetRenterShard(ctxParams *ContextParams, ssId string, hash string) (*RenterShard, error) {
	k := fmt.Sprintf(renterShardsInMemKey, ctxParams.n.Identity.Pretty(), ssId, hash)
	rs := &RenterShard{}
	if tmp, ok := renterShardsInMem.Get(k); ok {
		log.Debugf("get renter_shard:%s from cache.", k)
		rs = tmp.(*RenterShard)
	} else {
		log.Debugf("new renter_shard:%s.", k)
		ctx, _ := storage.NewGoContext(ctxParams.ctx)
		rs = &RenterShard{
			peerId: ctxParams.n.Identity.Pretty(),
			ssId:   ssId,
			hash:   hash,
			ctx:    ctx,
			ds:     ctxParams.n.Repo.Datastore(),
		}
		renterShardsInMem.Set(k, rs)
	}
	status, err := rs.status()
	if err != nil {
		return nil, err
	}
	rs.fsm = fsm.NewFSM(status.Status, renterShardFsmEvents, fsm.Callbacks{
		"enter_state": rs.enterState,
	})
	return rs, nil
}

func (rs *RenterShard) init() error {
	return ds.Save(rs.ds, fmt.Sprintf(renterShardStatusKey, rs.peerId, rs.ssId, rs.hash), &renterpb.RenterShardStatus{
		Status:      rshInitStatus,
		Message:     "",
		LastUpdated: time.Now().UTC(),
	})
}

func (rs *RenterShard) status() (*renterpb.RenterShardStatus, error) {
	status := &renterpb.RenterShardStatus{}
	err := ds.Get(rs.ds, fmt.Sprintf(renterShardStatusKey, rs.peerId, rs.ssId, rs.hash), status)
	if err == datastore.ErrNotFound {
		return &renterpb.RenterShardStatus{
			Status:      rssInitStatus,
			Message:     "",
			LastUpdated: time.Now().UTC(),
		}, nil
	}
	return status, err
}

func (rs *RenterShard) contracts() (*renterpb.RenterShardSignedContracts, error) {
	contracts := &renterpb.RenterShardSignedContracts{}
	err := ds.Get(rs.ds, fmt.Sprintf(renterShardContractsKey, rs.peerId, rs.ssId, rs.hash), contracts)
	if err == datastore.ErrNotFound {
		return contracts, nil
	}
	return contracts, err
}

func (rs *RenterShard) contract(signedEscrowContract []byte, signedGuardContract []byte) error {
	err := rs.fsm.Event(rshToContractEvent)
	if err != nil {
		log.Errorf("do contract error:%s", err.Error())
		return err
	}
	status := &renterpb.RenterShardStatus{
		Status:      rshContractStatus,
		LastUpdated: time.Now().UTC(),
	}
	signedContracts := &renterpb.RenterShardSignedContracts{
		SignedEscrowContract: signedEscrowContract,
		SignedGuardContract:  signedGuardContract,
	}
	return ds.Batch(rs.ds, []string{
		fmt.Sprintf(renterShardStatusKey, rs.peerId, rs.ssId, rs.hash),
		fmt.Sprintf(renterShardContractsKey, rs.peerId, rs.ssId, rs.hash),
	}, []proto.Message{
		status, signedContracts,
	})
}

func (rs *RenterShard) enterState(e *fsm.Event) {
	// FIXME: log.Info
	fmt.Printf("shard: %s:%s enter status: %s", rs.ssId, rs.hash, e.Dst)
}

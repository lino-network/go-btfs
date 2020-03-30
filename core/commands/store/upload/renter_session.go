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
	"time"
)

const (
	rssInitStatus   = "init"
	rssSubmitStatus = "submit"

	rssToSubmitEvent = "to-submit-event"

	renterSessionKey       = "/btfs/%s/renter/sessions/%s/"
	renterSessionInMemKey  = renterSessionKey
	renterSessionStatusKey = renterSessionKey + "status"
)

var (
	renterSessionsInMem = cmap.New()
	fsmEvents           = fsm.Events{
		{Name: rssToSubmitEvent, Src: []string{rssInitStatus}, Dst: rssSubmitStatus},
	}
)

type RenterSession struct {
	peerId    string
	ssId      string
	hash      string
	fsm       *fsm.FSM
	ctxParams *ContextParams
	ctx       context.Context
	cancel    context.CancelFunc
}

func GetRenterSession(ctxParams *ContextParams, ssId string, hash string) (*RenterSession,
	error) {
	k := fmt.Sprintf(renterSessionInMemKey, ctxParams.n.Identity.Pretty(), ssId)
	var rs *RenterSession
	if tmp, ok := renterShardsInMem.Get(k); ok {
		log.Debugf("get renter_session:%s from cache.", k)
		fmt.Println("get renter_session from cache.", k)
		rs = tmp.(*RenterSession)
	} else {
		log.Debugf("new renter_session:%s.", k)
		fmt.Println("new renter_session.", k)
		ctx, cancel := storage.NewGoContext(ctxParams.ctx)
		rs = &RenterSession{
			peerId:    ctxParams.n.Identity.Pretty(),
			ssId:      ssId,
			hash:      hash,
			ctx:       ctx,
			cancel:    cancel,
			ctxParams: ctxParams,
		}
		status, err := rs.status()
		if err != nil {
			return nil, err
		}
		rs.fsm = fsm.NewFSM(status.Status, renterShardFsmEvents, fsm.Callbacks{
			"enter_state": rs.enterState,
		})
		renterShardsInMem.Set(k, rs)
	}
	return rs, nil
}

func (rs *RenterSession) init(shardHashes []string) error {
	return ds.Save(rs.ctxParams.n.Repo.Datastore(), fmt.Sprintf(renterSessionStatusKey, rs.peerId, rs.ssId),
		&renterpb.RenterSessionStatus{
			Status:      rssInitStatus,
			Message:     "",
			LastUpdated: time.Now().UTC(),
			ShardHashes: shardHashes,
		})
}

func (rs *RenterSession) status() (*renterpb.RenterSessionStatus, error) {
	status := &renterpb.RenterSessionStatus{}
	err := ds.Get(rs.ctxParams.n.Repo.Datastore(), fmt.Sprintf(renterSessionStatusKey, rs.peerId, rs.ssId), status)
	if err == datastore.ErrNotFound {
		return &renterpb.RenterSessionStatus{
			Status:      rssInitStatus,
			Message:     "",
			LastUpdated: time.Now().UTC(),
			ShardHashes: make([]string, 0),
		}, nil
	}
	return status, err
}

func (rs *RenterSession) submit() {
	rs.fsm.Event(rssSubmitStatus)
}

func (rs *RenterSession) enterState(e *fsm.Event) {
	// FIXME: log.Info
	fmt.Printf("session: %s enter status: %s", rs.ssId, e.Dst)
}

func (rs *RenterSession) GetCompleteShardsNum() (int, int, error) {
	var completeNum, errorNum int
	status, err := rs.status()
	if err != nil {
		return 0, 0, err
	}
	for _, h := range status.ShardHashes {
		shard, err := GetRenterShard(rs.ctxParams, rs.ssId, h)
		if err != nil {
			log.Errorf("get renter shard error:", err.Error())
			continue
		}
		s, err := shard.status()
		if err != nil {
			log.Errorf("get renter shard status error:", err.Error())
			continue
		}
		fmt.Println("s.Status", s.Status)
		if s.Status == rshContractStatus {
			completeNum++
		} else if status.Status == rshErrorStatus {
			errorNum++
			return completeNum, errorNum, nil
		}
	}
	return completeNum, errorNum, nil
}

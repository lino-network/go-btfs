package upload

import (
	"fmt"
	cmap "github.com/orcaman/concurrent-map"
	"github.com/tron-us/go-btfs-common/crypto"
	"github.com/tron-us/protobuf/proto"
	"time"

	config "github.com/TRON-US/go-btfs-config"
	"github.com/TRON-US/go-btfs/core/escrow"
	guardPb "github.com/tron-us/go-btfs-common/protos/guard"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/prometheus/common/log"
)

var (
	guardChanMaps = cmap.New()
)

type ContractParams struct {
	ContractId    string
	RenterPid     string
	HostPid       string
	ShardIndex    int32
	ShardHash     string
	ShardSize     int64
	FileHash      string
	StartTime     time.Time
	StorageLength int64
	Price         int64
	TotalPay      int64
}

func renterSignGuardContract(ctxParams *ContextParams, params *ContractParams) ([]byte, error) {
	guardPid, escrowPid, err := getGuardAndEscrowPid(ctxParams.cfg)
	if err != nil {
		return nil, err
	}
	gm := &guardPb.ContractMeta{
		ContractId:    params.ContractId,
		RenterPid:     params.RenterPid,
		HostPid:       params.HostPid,
		ShardHash:     params.ShardHash,
		ShardIndex:    params.ShardIndex,
		ShardFileSize: params.ShardSize,
		FileHash:      params.FileHash,
		RentStart:     params.StartTime,
		RentEnd:       params.StartTime.Add(time.Duration(params.StorageLength*24) * time.Hour),
		GuardPid:      guardPid.Pretty(),
		EscrowPid:     escrowPid.Pretty(),
		Price:         params.Price,
		Amount:        params.TotalPay, // TODO: CHANGE and aLL other optional fields
	}
	cont := &guardPb.Contract{
		ContractMeta:   *gm,
		LastModifyTime: time.Now(),
	}
	cont.RenterPid = ctxParams.n.Identity.String()
	cont.PreparerPid = cont.RenterPid
	bc := make(chan []byte)
	guardChanMaps.Set(gm.ContractId, bc)
	//FIXME
	onlineSigning := true
	if onlineSigning {
		go func() {
			sign, err := crypto.Sign(ctxParams.n.PrivateKey, gm)
			if err != nil {
				// TODO: error
				return
			}
			bc <- sign
		}()
	}
	signedBytes := <-bc
	cont.RenterSignature = signedBytes
	return proto.Marshal(cont)
}

func getGuardAndEscrowPid(configuration *config.Config) (peer.ID, peer.ID, error) {
	escrowPubKeys := configuration.Services.EscrowPubKeys
	if len(escrowPubKeys) == 0 {
		return "", "", fmt.Errorf("missing escrow public key in config")
	}
	guardPubKeys := configuration.Services.GuardPubKeys
	if len(guardPubKeys) == 0 {
		return "", "", fmt.Errorf("missing guard public key in config")
	}
	escrowPid, err := pidFromString(escrowPubKeys[0])
	if err != nil {
		log.Error("parse escrow config failed", escrowPubKeys[0])
		return "", "", err
	}
	guardPid, err := pidFromString(guardPubKeys[0])
	if err != nil {
		log.Error("parse guard config failed", guardPubKeys[1])
		return "", "", err
	}
	return guardPid, escrowPid, err
}

func pidFromString(key string) (peer.ID, error) {
	pubKey, err := escrow.ConvertPubKeyFromString(key)
	if err != nil {
		return "", err
	}
	return peer.IDFromPublicKey(pubKey)
}

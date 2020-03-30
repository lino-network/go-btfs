package upload

import (
	"fmt"
	"github.com/TRON-US/go-btfs/core/escrow"
	"github.com/tron-us/go-btfs-common/crypto"

	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p-core/peer"
	cmap "github.com/orcaman/concurrent-map"
)

var (
	escrowChanMaps = cmap.New()
)

func renterSignEscrowContract(params *ContextParams, host string, tp int64) ([]byte, error) {
	hostPid, err := peer.IDB58Decode(host)
	if err != nil {
		return nil, err
	}
	contractId := uuid.New().String()
	escrowContract, err := escrow.NewContract(params.cfg, contractId, params.n, hostPid, tp, false, 0, "")
	if err != nil {
		return nil, fmt.Errorf("create escrow contract failed: [%v] ", err)
	}
	bc := make(chan []byte)
	escrowChanMaps.Set(escrowContract.ContractId, bc)
	//FIXME
	onlineSigning := true
	if onlineSigning {
		go func() {
			sign, err := crypto.Sign(params.n.PrivateKey, escrowContract)
			if err != nil {
				// TODO: error
				return
			}
			bc <- sign
		}()
	}
	renterSignBytes := <-bc
	renterSignedEscrowContract, err := escrow.SignContractAndMarshalOffSign(escrowContract, renterSignBytes, nil, true)
	if err != nil {
		return nil, err
	}
	return renterSignedEscrowContract, nil
}

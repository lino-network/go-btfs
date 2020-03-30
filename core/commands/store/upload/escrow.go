package upload

import (
	"context"
	"encoding/base64"
	"fmt"
	config "github.com/TRON-US/go-btfs-config"
	"github.com/TRON-US/go-btfs/core/escrow"
	"github.com/ethereum/go-ethereum/log"
	ic "github.com/libp2p/go-libp2p-core/crypto"
	"github.com/tron-us/go-btfs-common/crypto"
	escrowpb "github.com/tron-us/go-btfs-common/protos/escrow"
	ledgerpb "github.com/tron-us/go-btfs-common/protos/ledger"
	"github.com/tron-us/go-btfs-common/utils/grpc"
	"github.com/tron-us/protobuf/proto"

	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p-core/peer"
	cmap "github.com/orcaman/concurrent-map"
)

var (
	escrowChanMaps = cmap.New()
)

func renterSignEscrowContract(params *ContextParams, host string, tp int64, offlineSigning bool) ([]byte, error) {
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
	if !offlineSigning {
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

func balance(ctx context.Context, configuration *config.Config) (int64, error) {
	lgSignedPubKey := &ledgerpb.SignedPublicKey{
		Key:       nil,
		Signature: nil,
	}
	var balance int64 = 0
	err := grpc.EscrowClient(configuration.Services.EscrowDomain).WithContext(ctx,
		func(ctx context.Context, client escrowpb.EscrowServiceClient) error {
			res, err := client.BalanceOf(ctx, &ledgerpb.SignedCreateAccountRequest{
				Key:       lgSignedPubKey.Key,
				Signature: lgSignedPubKey.Signature,
			})
			if err != nil {
				return err
			}
			err = verifyEscrowRes(configuration, res.Result, res.EscrowSignature)
			if err != nil {
				return err
			}
			balance = res.Result.Balance
			log.Debug("balanceof account is ", balance)
			return nil
		})
	if err != nil {
		return 0, err
	}
	return balance, nil
}

func verifyEscrowRes(configuration *config.Config, message proto.Message, sig []byte) error {
	escrowPubkey, err := convertPubKeyFromString(configuration.Services.EscrowPubKeys[0])
	if err != nil {
		return err
	}
	ok, err := crypto.Verify(escrowPubkey, message, sig)
	if err != nil || !ok {
		return fmt.Errorf("verify escrow failed %v", err)
	}
	return nil
}

func convertPubKeyFromString(pubKeyStr string) (ic.PubKey, error) {
	raw, err := base64.StdEncoding.DecodeString(pubKeyStr)
	if err != nil {
		return nil, err
	}
	return ic.UnmarshalPublicKey(raw)
}

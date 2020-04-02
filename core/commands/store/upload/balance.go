package upload

import (
	"context"
	"fmt"
	config "github.com/TRON-US/go-btfs-config"
	"github.com/TRON-US/go-btfs/core/commands/storage"
	cmap "github.com/orcaman/concurrent-map"
	"github.com/tron-us/go-btfs-common/ledger"
	escrowpb "github.com/tron-us/go-btfs-common/protos/escrow"
	ledgerpb "github.com/tron-us/go-btfs-common/protos/ledger"
	"github.com/tron-us/go-btfs-common/utils/grpc"
	"github.com/tron-us/protobuf/proto"
)

const (
	Text = iota + 1
	Base64
)

var (
	balanceChanMaps = cmap.New()
)

func checkBalance(rss *RenterSession, offlineSigning bool, totalPay int64) error {
	bc := make(chan []byte)
	balanceChanMaps.Set(rss.ssId, bc)
	if !offlineSigning {
		go func() {
			privKey, err := rss.ctxParams.cfg.Identity.DecodePrivateKey("")
			if err != nil {
				if err != nil {
					// TODO: error
					return
				}
			}
			lgSignedPubKey, err := ledger.NewSignedPublicKey(privKey, privKey.GetPublic())
			if err != nil {
				if err != nil {
					// TODO: error
					return
				}
			}
			signedBytes, err := proto.Marshal(lgSignedPubKey)
			if err != nil {
				// TODO: error
				return
			}
			bc <- signedBytes
		}()
	}
	signedBytes := <-bc
	balance, err := balanceHelper(rss.ctxParams.ctx, rss.ctxParams.cfg, signedBytes)
	if err != nil {
		return err
	}
	if balance < totalPay {
		return fmt.Errorf("not enough balance to submit contract, current balance is [%v]", balance)
	}
	return nil
}

func balanceHelper(ctx context.Context, configuration *config.Config, signedBytes []byte) (int64, error) {
	var ledgerSignedPubKey ledgerpb.SignedPublicKey
	err := proto.Unmarshal(signedBytes, &ledgerSignedPubKey)
	if err != nil {
		return 0, err
	}
	var balance int64 = 0
	ctx, _ = storage.NewGoContext(ctx)
	err = grpc.EscrowClient(configuration.Services.EscrowDomain).WithContext(ctx,
		func(ctx context.Context, client escrowpb.EscrowServiceClient) error {
			res, err := client.BalanceOf(ctx, &ledgerpb.SignedCreateAccountRequest{
				Key:       ledgerSignedPubKey.Key,
				Signature: ledgerSignedPubKey.Signature,
			})
			if err != nil {
				return err
			}
			err = verifyEscrowRes(configuration, res.Result, res.EscrowSignature)
			if err != nil {
				return err
			}
			balance = res.Result.Balance
			return nil
		})
	if err != nil {
		return 0, err
	}
	return balance, nil
}

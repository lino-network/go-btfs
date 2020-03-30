package upload

import (
	"context"
	"fmt"
	cmds "github.com/TRON-US/go-btfs-cmds"
	config "github.com/TRON-US/go-btfs-config"
	"github.com/TRON-US/go-btfs/core"
	"github.com/TRON-US/go-btfs/core/commands/cmdenv"
	"github.com/TRON-US/go-btfs/core/commands/storage"
	"github.com/TRON-US/go-btfs/core/commands/store/upload/helper"
	"github.com/TRON-US/go-btfs/core/corehttp/remote"
	iface "github.com/TRON-US/interface-go-btfs-core"
	"github.com/alecthomas/units"
	"github.com/cenkalti/backoff/v3"
	"github.com/google/uuid"
	cidlib "github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p-core/peer"
	"time"
)

var storageUploadOfflineCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Store files on BTFS network nodes through BTT payment via offline signing.",
		ShortDescription: `
Upload a file with offline signing. I.e., SDK application acts as renter.`,
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("file-hash", true, false, "Hash of file to upload."),
		cmds.StringArg("offline-peer-id", true, false, "Peer id when offline upload."),
		cmds.StringArg("offline-nonce-ts", true, false, "Nounce timestamp when offline upload."),
		cmds.StringArg("offline-signature", true, false, "Session signature when offline upload."),
	},
	RunTimeout: 15 * time.Minute,
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		ssId := uuid.New().String()
		ctxParams, err := extractContextParams(req, env)
		if err != nil {
			return err
		}
		fileHash := req.Arguments[0]
		offlinePeerId := req.Arguments[1]
		shardHashes, fileSize, shardSize, err := getShardHashes(ctxParams, fileHash)
		if err != nil {
			return err
		}
		fmt.Println("fileSize", fileSize, "shardSize", shardSize)
		price, storageLength, err := getPriceAndMinStorageLength(ctxParams)
		if err != nil {
			return err
		}
		hp := getHostsProvider(ctxParams)
		for shardIndex, shardHash := range shardHashes {
			go func(i int, h string) {
				rs := NewRenterShard(ssId, h)
				backoff.Retry(func() error {
					host, err := hp.NextValidHost(price)
					if err != nil {
						return err
					}
					tp := totalPay(shardSize, price, storageLength)
					escrowCotractBytes, err := renterSignEscrowContract(ctxParams, host, tp)
					if err != nil {
						return err
					}
					guardContractBytes, err := renterSignGuardContract(ctxParams, &ContractParams{
						ContractId:    ssId,
						RenterPid:     ctxParams.n.Identity.Pretty(),
						HostPid:       host,
						ShardIndex:    int32(i),
						ShardHash:     h,
						ShardSize:     shardSize,
						FileHash:      fileHash,
						StartTime:     time.Now(),
						StorageLength: int64(storageLength),
						Price:         price,
						TotalPay:      tp,
					})
					if err != nil {
						return err
					}
					hostPid, err := peer.IDB58Decode(host)
					if err != nil {
						return err
					}
					_, err = remote.P2PCall(ctxParams.ctx, ctxParams.n, hostPid, "/storage/upload/init",
						ssId,
						fileHash,
						h,
						price,
						escrowCotractBytes,
						guardContractBytes,
						storageLength,
						shardSize,
						i,
						offlinePeerId,
					)
					if err != nil {
						return err
					}
					return nil
				}, handleShardBo)
			}(shardIndex, shardHash)
		}
		seRes := &UploadRes{
			ID: ssId,
		}
		return res.Emit(seRes)
	},
	Type: UploadRes{},
}

type ContextParams struct {
	req *cmds.Request
	ctx context.Context
	n   *core.IpfsNode
	cfg *config.Config
	api iface.CoreAPI
}

func extractContextParams(req *cmds.Request, env cmds.Environment) (*ContextParams, error) {
	// get config settings
	cfg, err := cmdenv.GetConfig(env)
	if err != nil {
		return nil, err
	}
	// get node
	n, err := cmdenv.GetNode(env)
	if err != nil {
		return nil, err
	}
	// get core api
	api, err := cmdenv.GetApi(env, req)
	if err != nil {
		return nil, err
	}
	return &ContextParams{
		req: req,
		ctx: req.Context,
		n:   n,
		cfg: cfg,
		api: api,
	}, nil
}

func getShardHashes(params *ContextParams, fileHash string) (shardHashes []string, fileSize int64,
	shardSize int64, err error) {
	fileCid, err := cidlib.Parse(fileHash)
	if err != nil {
		return nil, -1, -1, err
	}
	cids, fileSize, err := storage.CheckAndGetReedSolomonShardHashes(params.ctx, params.n, params.api, fileCid)
	if err != nil || len(cids) == 0 {
		return nil, -1, -1, fmt.Errorf("invalid hash: %s", err)
	}

	shardHashes = make([]string, 0)
	for _, c := range cids {
		shardHashes = append(shardHashes, c.String())
	}
	shardCid, err := cidlib.Parse(shardHashes[0])
	if err != nil {
		return nil, -1, -1, err
	}
	sz, err := helper.GetNodeSizeFromCid(params.ctx, shardCid, params.api)
	if err != nil {
		return nil, -1, -1, err
	}
	shardSize = int64(sz)
	return
}

func getPriceAndMinStorageLength(params *ContextParams) (price int64, storageLength int, err error) {
	ns, err := storage.GetHostStorageConfig(params.ctx, params.n)
	if err != nil {
		return -1, -1, err
	}
	price, found := params.req.Options[uploadPriceOptionName].(int64)
	if !found {
		price = int64(ns.StoragePriceAsk)
	}
	storageLength = params.req.Options[storageLengthOptionName].(int)
	if uint64(storageLength) < ns.StorageTimeMin {
		return -1, -1, fmt.Errorf("invalid storage len. want: >= %d, got: %d",
			ns.StorageTimeMin, storageLength)
	}
	return
}

func totalPay(shardSize int64, price int64, storageLength int) int64 {
	totalPay := int64(float64(shardSize) / float64(units.GiB) * float64(price) * float64(storageLength))
	if totalPay <= 0 {
		totalPay = 1
	}
	return totalPay
}

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
	"github.com/prometheus/common/log"
	"time"
)

var StorageUploadOfflineCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Store files on BTFS network nodes through BTT payment via offline signing.",
		ShortDescription: `
Upload a file with offline signing. I.e., SDK application acts as renter.`,
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("file-hash", true, false, "Hash of file to upload."),
		cmds.StringArg("offline-peer-id", false, false, "Peer id when offline upload."),
		cmds.StringArg("offline-nonce-ts", false, false, "Nounce timestamp when offline upload."),
		cmds.StringArg("offline-signature", false, false, "Session signature when offline upload."),
	},
	RunTimeout: 15 * time.Minute,
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		ssId := uuid.New().String()
		ctxParams, err := extractContextParams(req, env)
		if err != nil {
			return err
		}
		fileHash := req.Arguments[0]
		offlinePeerId := ctxParams.n.Identity.Pretty()
		offlineSigning := false
		if len(req.Arguments) > 1 {
			offlinePeerId = req.Arguments[1]
			offlineSigning = true
		}
		shardHashes, fileSize, shardSize, err := getShardHashes(ctxParams, fileHash)
		if err != nil {
			return err
		}
		price, storageLength, err := getPriceAndMinStorageLength(ctxParams)
		if err != nil {
			return err
		}
		hp := getHostsProvider(ctxParams)
		rss, err := GetRenterSession(ctxParams, ssId, fileHash)
		if err != nil {
			return err
		}
		err = rss.init(shardHashes)
		if err != nil {
			return err
		}
		for shardIndex, shardHash := range shardHashes {
			rsh, err := GetRenterShard(ctxParams, ssId, shardHash)
			if err != nil {
				return err
			}
			err = rsh.init()
			if err != nil {
				return err
			}
			go func(i int, h string, rsh *RenterShard) {
				backoff.Retry(func() error {
					select {
					case <-rss.ctx.Done():
						return nil
					default:
						break
					}
					host, err := hp.NextValidHost(price)
					if err != nil {
						//TODO: status -> Error
						rss.cancel()
						return err
					}
					tp := totalPay(shardSize, price, storageLength)
					escrowCotractBytes, err := renterSignEscrowContract(ctxParams, host, tp, offlineSigning)
					if err != nil {
						log.Errorf("shard %s signs escrow_contract error: %s", h, err.Error())
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
					}, offlineSigning)
					if err != nil {
						log.Errorf("shard %s signs guard_contract error: %s", h, err.Error())
						return err
					}
					hostPid, err := peer.IDB58Decode(host)
					if err != nil {
						log.Errorf("shard %s decodes host_pid error: %s", h, err.Error())
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
						log.Errorf("shard %s p2pcall error: %s", h, err.Error())
						return err
					}
					return nil
				}, handleShardBo)
			}(shardIndex, shardHash, rsh)
		}
		// waiting for contracts of 30 shards
		go func(rss *RenterSession, numShards int) {
			tick := time.Tick(5 * time.Second)
			for true {
				select {
				case <-tick:
					completeNum, errorNum, err := rss.GetCompleteShardsNum()
					if err != nil {
						continue
					}
					//TODO log.info
					fmt.Println("session", rss.ssId, "completeNum", completeNum, "errorNum", errorNum)
					if completeNum == numShards {
						submit(rss, fileSize)
						return
					} else if errorNum > 0 {
						//TODO
						//rss.Error(errors.New("there are error shards"))
						//TODO log.info
						fmt.Println("session", rss.ssId, "errorNum", errorNum)
						return
					}
				case <-rss.ctx.Done():
					log.Infof("session %s done", rss.ssId)
					return
				}
			}
		}(rss, len(shardHashes))
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

func submit(rss *RenterSession, fileSize int64) {
	rss.submit()
	return
}

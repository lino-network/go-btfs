package upload

import (
	"fmt"
	"github.com/gogo/protobuf/proto"
	guardPb "github.com/tron-us/go-btfs-common/protos/guard"

	cmds "github.com/TRON-US/go-btfs-cmds"
)

var StorageUploadStatusCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Check storage upload and payment status (From client's perspective).",
		ShortDescription: `
This command print upload and payment status by the time queried.`,
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("session-id", true, false, "ID for the entire storage upload session.").EnableStdin(),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		status := &StatusRes{}
		// check and get session info from sessionMap
		ssId := req.Arguments[0]
		ctxParams, err := extractContextParams(req, env)
		if err != nil {
			return err
		}
		// check if checking request from host or client
		if !ctxParams.cfg.Experimental.StorageClientEnabled && !ctxParams.cfg.Experimental.StorageHostEnabled {
			return fmt.Errorf("storage client/host api not enabled")
		}
		rss, err := GetRenterSession(ctxParams, ssId, "")
		if err != nil {
			return err
		}
		s, err := rss.status()
		if err != nil {
			return err
		}
		status.Status = s.Status
		status.Message = s.Message

		// get shards info from session
		shards := make(map[string]*ShardStatus)
		for _, h := range s.ShardHashes {
			sh, err := GetRenterShard(ctxParams, ssId, h)
			if err != nil {
				return err
			}
			sht, err := sh.status()
			if err != nil {
				return err
			}
			contracts, err := sh.contracts()
			if err != nil {
				return err
			}
			c := &ShardStatus{
				ContractID: "",
				Price:      0,
				Host:       "",
				Status:     sht.Status,
				Message:    sht.Message,
			}
			if contracts.SignedGuardContract != nil {
				var guardContract *guardPb.ContractMeta
				err := proto.Unmarshal(contracts.SignedGuardContract, guardContract)
				if err != nil {
					return err
				}
				c.ContractID = guardContract.ContractId
				c.Price = guardContract.Price
				c.Host = guardContract.HostPid
			}
			shards[h] = c
		}
		status.Shards = shards
		return res.Emit(status)
	},
	Type: StatusRes{},
}

type StatusRes struct {
	Status   string
	Message  string
	FileHash string
	Shards   map[string]*ShardStatus
}

type ShardStatus struct {
	ContractID string
	Price      int64
	Host       string
	Status     string
	Message    string
}

package upload

import (
	"fmt"
	cmds "github.com/TRON-US/go-btfs-cmds"
	"github.com/prometheus/common/log"
)

var StorageUploadRecvContractCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "For renter client to receive half signed contracts.",
	},
	Arguments: []cmds.Argument{
		cmds.StringArg("session-id", true, false, "Session ID which renter uses to store all shards information."),
		cmds.StringArg("shard-hash", true, false, "Shard the storage node should fetch."),
		cmds.StringArg("shard-index", true, false, "Index of shard within the encoding scheme."),
		cmds.StringArg("escrow-contract", true, false, "Signed Escrow contract."),
		cmds.StringArg("guard-contract", true, false, "Signed Guard contract."),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		ssId := req.Arguments[0]
		shardHash := req.Arguments[1]
		//TODO: log.debugf
		fmt.Println("recv contract", ssId, shardHash)
		ctxParams, err := extractContextParams(req, env)
		if err != nil {
			log.Errorf("recv contract err:%s", err.Error())
			return err
		}
		rs, err := GetRenterShard(ctxParams, ssId, shardHash)
		if err != nil {
			log.Errorf("recv contract err:%s", err.Error())
			return err
		}
		err = rs.contract([]byte(req.Arguments[3]), []byte(req.Arguments[4]))
		if err != nil {
			log.Errorf("recv contract err:%s", err.Error())
			return err
		}
		return nil
	},
}

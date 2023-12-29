package history

import (
	"context"
	"fmt"
	"time"

	"github.com/breez/lspd/common"
)

type Server struct {
	UnimplementedHistoryServer
	store   Store
	nodeids [][]byte
}

func NewServer(store Store, nodes []*common.Node) *Server {
	nodeids := make([][]byte, len(nodes))
	for i, node := range nodes {
		nodeids[i] = node.NodeId
	}
	return &Server{
		store:   store,
		nodeids: nodeids,
	}
}

func (s *Server) ExportForwards(
	ctx context.Context,
	req *ExportForwardsRequest,
) (*ExportForwardsResponse, error) {
	start := time.Unix(req.Start, 0)
	end := time.Unix(req.End, 0)
	forwards, err := s.store.ExportTokenForwardsForExternalNode(ctx, start, end, req.Nodeid, req.ExternalNodeid)
	if err != nil {
		return nil, err
	}

	result := make([]*ExportedForward, len(forwards))
	for _, forward := range forwards {
		result = append(result, &ExportedForward{
			Token:          forward.Token,
			Nodeid:         forward.NodeId,
			ExternalNodeid: forward.ExternalNodeId,
			ResolvedTime:   forward.ResolvedTime.UnixNano(),
			Direction:      forward.Direction,
			AmountMsat:     forward.AmountMsat,
		})
	}

	return &ExportForwardsResponse{
		Forwards: result,
	}, nil
}

func (s *Server) ImportExternalForwards(
	ctx context.Context,
	req *ImportExternalForwardsRequest,
) (*ImportExternalForwardsResponse, error) {
	if req.Forwards == nil || len(req.Forwards) == 0 {
		return &ImportExternalForwardsResponse{}, nil
	}

	// TODO: Make sure we're hosting the external nodeid for each forward

	forwards := make([]*ExternalTokenForward, len(req.Forwards))
	for _, forward := range req.Forwards {
		if forward.Direction != "send" && forward.Direction != "receive" {
			return nil, fmt.Errorf("invalid direction '%s'", forward.Direction)
		}
		// Note: their external node id is our internal node id.
		forwards = append(forwards, &ExternalTokenForward{
			Token:          forward.Token,
			NodeId:         forward.ExternalNodeid,
			ExternalNodeId: forward.Nodeid,
			ResolvedTime:   time.Unix(0, int64(forward.ResolvedTime)),
			Direction:      forward.Direction,
			AmountMsat:     forward.AmountMsat,
		})
	}

	err := s.store.ImportTokenForwards(ctx, forwards)
	if err != nil {
		return nil, err
	}
	return &ImportExternalForwardsResponse{}, nil
}

package itest

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"github.com/breez/lspd/itest/lntest"
)

type RecommendedFeesResponse struct {
	FastestFee  float64 `json:"fastestFee"`
	HalfHourFee float64 `json:"halfHourFee"`
	HourFee     float64 `json:"hourFee"`
	EconomyFee  float64 `json:"economyFee"`
	MinimumFee  float64 `json:"minimumFee"`
}

type mempoolApi struct {
	addr string
	h    *lntest.TestHarness
	fees *RecommendedFeesResponse
	lis  net.Listener
}

func NewMempoolApi(h *lntest.TestHarness) *mempoolApi {
	port, err := lntest.GetPort()
	if err != nil {
		h.T.Fatalf("Failed to get port for mempool api: %v", err)
	}

	return &mempoolApi{
		addr: fmt.Sprintf("127.0.0.1:%d", port),
		h:    h,
		fees: &RecommendedFeesResponse{
			MinimumFee:  5,
			EconomyFee:  5,
			HourFee:     5,
			HalfHourFee: 5,
			FastestFee:  5,
		},
	}
}

func (m *mempoolApi) Address() string {
	return fmt.Sprintf("http://%s/api/v1/", m.addr)
}

func (m *mempoolApi) SetFees(fees *RecommendedFeesResponse) {
	m.fees = fees
}

func (m *mempoolApi) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/fees/recommended", func(w http.ResponseWriter, r *http.Request) {
		j, err := json.Marshal(m.fees)
		if err != nil {
			m.h.T.Fatalf("Failed to marshal mempool fees: %v", err)
		}
		_, err = w.Write(j)
		if err != nil {
			m.h.T.Fatalf("Failed to write mempool response: %v", err)
		}
	})
	lis, err := net.Listen("tcp", m.addr)
	if err != nil {
		m.h.T.Fatalf("failed to start mempool api: %v", err)
	}

	m.lis = lis
	m.h.AddStoppable(m)

	go http.Serve(lis, mux)
}

func (m *mempoolApi) Stop() error {
	lis := m.lis
	if lis == nil {
		return nil
	}

	m.lis = nil
	return lis.Close()
}

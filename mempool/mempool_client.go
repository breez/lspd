package mempool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/breez/lspd/chain"
)

type MempoolClient struct {
	apiBaseUrl string
	httpClient *http.Client
}

type RecommendedFeesResponse struct {
	FastestFee  float64 `json:"fastestFee"`
	HalfHourFee float64 `json:"halfHourFee"`
	HourFee     float64 `json:"hourFee"`
	EconomyFee  float64 `json:"economyFee"`
	MinimumFee  float64 `json:"minimumFee"`
}

func NewMempoolClient(apiBaseUrl string) (*MempoolClient, error) {
	if apiBaseUrl == "" {
		return nil, fmt.Errorf("apiBaseUrl not set")
	}

	if !strings.HasSuffix(apiBaseUrl, "/") {
		apiBaseUrl = apiBaseUrl + "/"
	}

	return &MempoolClient{
		apiBaseUrl: apiBaseUrl,
		httpClient: http.DefaultClient,
	}, nil
}

func (m *MempoolClient) EstimateFeeRate(
	ctx context.Context,
	strategy chain.FeeStrategy,
) (*chain.FeeEstimation, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		m.apiBaseUrl+"fees/recommended",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("http.NewRequestWithContext error: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpClient.Do error: %w", err)
	}

	defer resp.Body.Close()
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil, fmt.Errorf("error statuscode %v: %w", resp.StatusCode, err)
	}

	var body RecommendedFeesResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	var rate float64
	switch strategy {
	case chain.FeeStrategyFastest:
		rate = body.FastestFee
	case chain.FeeStrategyHalfHour:
		rate = body.HalfHourFee
	case chain.FeeStrategyHour:
		rate = body.HourFee
	case chain.FeeStrategyEconomy:
		rate = body.EconomyFee
	case chain.FeeStrategyMinimum:
		rate = body.MinimumFee
	default:
		return nil, fmt.Errorf("unsupported fee strategy: %v", strategy)
	}

	return &chain.FeeEstimation{
		SatPerVByte: &rate,
	}, nil
}

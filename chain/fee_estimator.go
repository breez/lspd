package chain

import "context"

type FeeStrategy int

const (
	FeeStrategyFastest  FeeStrategy = 0
	FeeStrategyHalfHour FeeStrategy = 1
	FeeStrategyHour     FeeStrategy = 2
	FeeStrategyEconomy  FeeStrategy = 3
	FeeStrategyMinimum  FeeStrategy = 4
)

type FeeEstimation struct {
	SatPerVByte float64
}

type FeeEstimator interface {
	EstimateFeeRate(context.Context, FeeStrategy) (*FeeEstimation, error)
}

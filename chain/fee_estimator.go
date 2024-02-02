package chain

import (
	"context"
	"fmt"
)

type FeeStrategy int

const (
	FeeStrategyFastest  FeeStrategy = 0
	FeeStrategyHalfHour FeeStrategy = 1
	FeeStrategyHour     FeeStrategy = 2
	FeeStrategyEconomy  FeeStrategy = 3
	FeeStrategyMinimum  FeeStrategy = 4
)

type FeeEstimation struct {
	SatPerVByte *float64
	TargetConf  *uint32
}

func (f *FeeEstimation) String() string {
	feeStr := "<nil>"
	confStr := "<nil>"
	if f.SatPerVByte != nil {
		feeStr = fmt.Sprintf("%.5f", *f.SatPerVByte)
	}
	if f.TargetConf != nil {
		confStr = fmt.Sprintf("%v", *f.TargetConf)
	}
	return fmt.Sprintf("satPerVbyte %s, targetConf %s", feeStr, confStr)
}

type FeeEstimator interface {
	EstimateFeeRate(context.Context, FeeStrategy) (*FeeEstimation, error)
}

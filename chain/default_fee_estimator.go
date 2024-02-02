package chain

import "context"

type DefaultFeeEstimator struct {
	targetConf uint32
}

func NewDefaultFeeEstimator(targetConf uint32) *DefaultFeeEstimator {
	return &DefaultFeeEstimator{
		targetConf: targetConf,
	}
}

func (e *DefaultFeeEstimator) EstimateFeeRate(
	context.Context,
	FeeStrategy,
) (*FeeEstimation, error) {
	return &FeeEstimation{
		TargetConf: &e.targetConf,
	}, nil
}

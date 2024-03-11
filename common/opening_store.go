package common

import "time"

type OpeningFeeParamsSetting struct {
	Validity             time.Duration
	MinFeeMsat           uint64
	Proportional         uint32
	MinLifetime          uint32
	MaxClientToSelfDelay uint32
	MinPaymentSizeMsat   uint64
	MaxPaymentSizeMsat   uint64
}

type OpeningStore interface {
	GetFeeParamsSettings(token string) ([]*OpeningFeeParamsSetting, error)
}

package common

import "time"

type OpeningFeeParamsSetting struct {
	Validity time.Duration
	Params   *OpeningFeeParams
}

type OpeningFeeParams struct {
	MinFeeMsat           uint64 `json:"min_msat,string"`
	Proportional         uint32 `json:"proportional"`
	ValidUntil           string `json:"valid_until"`
	MinLifetime          uint32 `json:"max_idle_time"`
	MaxClientToSelfDelay uint32 `json:"max_client_to_self_delay"`
	MinPaymentSizeMsat   uint64 `json:"min_payment_size_msat,string"`
	MaxPaymentSizeMsat   uint64 `json:"max_payment_size_msat,string"`
	Promise              string `json:"promise"`
}

type OpeningStore interface {
	GetFeeParamsSettings(token string) ([]*OpeningFeeParamsSetting, error)
}

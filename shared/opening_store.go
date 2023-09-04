package shared

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
	Promise              string `json:"promise"`
}

type OpeningStore interface {
	GetFeeParamsSettings(token string) ([]*OpeningFeeParamsSetting, error)
}

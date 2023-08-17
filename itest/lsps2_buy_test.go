package itest

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"time"

	"github.com/breez/lntest"
	"github.com/breez/lspd/lsps0"
)

func testLsps2Buy(p *testParams) {
	SetFeeParams(p.Lsp(), []*FeeParamSetting{
		{
			Validity:     time.Second * 3600,
			MinMsat:      1000000,
			Proportional: 1000,
		},
	})
	p.BreezClient().Node().ConnectPeer(p.Lsp().LightningNode())

	p.BreezClient().Node().SendCustomMessage(&lntest.CustomMsgRequest{
		PeerId: hex.EncodeToString(p.Lsp().NodeId()),
		Type:   lsps0.Lsps0MessageType,
		Data: []byte(`{
			"method": "lsps2.get_info",
			"jsonrpc": "2.0",
			"id": "example#3cad6a54d302edba4c9ade2f7ffac098",
			"params": {
				"version": 1,
				"token": "hello"
			}
		  }`),
	})

	resp := p.BreezClient().ReceiveCustomMessage()
	log.Print(string(resp.Data))

	type params struct {
		MinFeeMsat           uint64 `json:"min_fee_msat,string"`
		Proportional         uint32 `json:"proportional"`
		ValidUntil           string `json:"valid_until"`
		MinLifetime          uint32 `json:"min_lifetime"`
		MaxClientToSelfDelay uint32 `json:"max_client_to_self_delay"`
		Promise              string `json:"promise"`
	}
	data := new(struct {
		Result struct {
			Menu       []params `json:"opening_fee_params_menu"`
			MinPayment uint64   `json:"min_payment_size_msat,string"`
			MaxPayment uint64   `json:"max_payment_size_msat,string"`
		} `json:"result"`
	})
	err := json.Unmarshal(resp.Data[:], &data)
	lntest.CheckError(p.t, err)

	pr, err := json.Marshal(&data.Result.Menu[0])
	lntest.CheckError(p.t, err)
	p.BreezClient().Node().SendCustomMessage(&lntest.CustomMsgRequest{
		PeerId: hex.EncodeToString(p.Lsp().NodeId()),
		Type:   lsps0.Lsps0MessageType,
		Data: append(append(
			[]byte(`{
			"method": "lsps2.get_info",
			"jsonrpc": "2.0",
			"id": "example#3cad6a54d302edba4c9ade2f7ffac098",
			"params": {
				"version": 1,
				"payment_size_msat": "42000",
				"opening_fee_params":`),
			pr...),
			[]byte(`}}`)...,
		),
	})

	buyResp := p.BreezClient().ReceiveCustomMessage()
	log.Print(string(resp.Data))
	b := new(struct {
		Result struct {
			Jit_channel_scid      string `json:"jit_channel_scid"`
			Lsp_cltv_expiry_delta uint32 `json:"lsp_cltv_expiry_delta"`
			Client_trusts_lsp     bool   `json:"client_trusts_lsp"`
		} `json:"result"`
	})
	err = json.Unmarshal(buyResp.Data, b)
	lntest.CheckError(p.t, err)
}

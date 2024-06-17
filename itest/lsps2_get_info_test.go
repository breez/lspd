package itest

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"time"

	"github.com/breez/lspd/itest/lntest"
	"github.com/breez/lspd/lsps0"
	"github.com/stretchr/testify/assert"
)

func testLsps2GetInfo(p *testParams) {
	p.Lspd().Client(0).SetFeeParams([]*FeeParamSetting{
		{
			Validity:     time.Second * 3600,
			MinMsat:      3000000,
			Proportional: 1000,
		},
		{
			Validity:     time.Second * 2800,
			MinMsat:      2000000,
			Proportional: 800,
		},
	})
	p.BreezClient().Node().ConnectPeer(p.Node())

	// Make sure everything is activated.
	<-time.After(htlcInterceptorDelay)

	rawMsg := `{
		"method": "lsps2.get_info",
		"jsonrpc": "2.0",
		"id": "example#3cad6a54d302edba4c9ade2f7ffac098",
		"params": {
			"version": 1,
			"token": "hello0"
		}
	  }`
	p.BreezClient().Node().SendCustomMessage(&lntest.CustomMsgRequest{
		PeerId: hex.EncodeToString(p.Node().NodeId()),
		Type:   lsps0.Lsps0MessageType,
		Data:   []byte(rawMsg),
	})

	resp := p.BreezClient().ReceiveCustomMessage()
	log.Print(string(resp.Data))
	assert.Equal(p.t, uint32(37913), resp.Type)

	content := make(map[string]json.RawMessage)
	err := json.Unmarshal(resp.Data[:], &content)
	lntest.CheckError(p.t, err)

	result := make(map[string]json.RawMessage)
	err = json.Unmarshal(content["result"], &result)
	lntest.CheckError(p.t, err)

	menu := []*struct {
		MinFeeMsat           uint64 `json:"min_fee_msat,string"`
		Proportional         uint32 `json:"proportional"`
		ValidUntil           string `json:"valid_until"`
		MinLifetime          uint32 `json:"min_lifetime"`
		MaxClientToSelfDelay uint32 `json:"max_client_to_self_delay"`
		Promise              string `json:"promise"`
	}{}
	err = json.Unmarshal(result["opening_fee_params_menu"], &menu)
	lntest.CheckError(p.t, err)

	assert.Len(p.t, menu, 2)
	assert.Equal(p.t, uint64(2000000), menu[0].MinFeeMsat)
	assert.Equal(p.t, uint64(3000000), menu[1].MinFeeMsat)
}

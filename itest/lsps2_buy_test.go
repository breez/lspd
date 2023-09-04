package itest

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"time"

	"github.com/breez/lntest"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lsps0"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/stretchr/testify/assert"
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
			"method": "lsps2.buy",
			"jsonrpc": "2.0",
			"id": "example#3cad6a54d302edba4c9ade2f7ffac099",
			"params": {
				"version": 1,
				"payment_size_msat": "42000000",
				"opening_fee_params":`),
			pr...),
			[]byte(`}}`)...,
		),
	})

	buyResp := p.BreezClient().ReceiveCustomMessage()
	log.Print(string(buyResp.Data))
	b := new(struct {
		Result struct {
			Jit_channel_scid      string `json:"jit_channel_scid"`
			Lsp_cltv_expiry_delta uint32 `json:"lsp_cltv_expiry_delta"`
			Client_trusts_lsp     bool   `json:"client_trusts_lsp"`
		} `json:"result"`
	})
	err = json.Unmarshal(buyResp.Data, b)
	lntest.CheckError(p.t, err)

	pgxPool, err := pgxpool.Connect(p.h.Ctx, p.lsp.PostgresBackend().ConnectionString())
	if err != nil {
		p.h.T.Fatalf("Failed to connect to postgres backend: %v", err)
	}
	defer pgxPool.Close()

	scid, err := lightning.NewShortChannelIDFromString(b.Result.Jit_channel_scid)
	lntest.CheckError(p.t, err)

	rows, err := pgxPool.Query(
		p.h.Ctx,
		`SELECT token 
		 FROM lsps2.buy_registrations
		 WHERE scid = $1`,
		int64(uint64(*scid)),
	)
	if err != nil {
		p.h.T.Fatalf("Failed to query token: %v", err)
	}

	defer rows.Close()
	if !rows.Next() {
		p.h.T.Fatal("No rows found")
	}

	var actual string
	err = rows.Scan(&actual)
	if err != nil {
		p.h.T.Fatalf("Failed to get token from row: %v", err)
	}

	assert.Equal(p.h.T, "hello", actual)
}

package itest

import (
	"encoding/hex"
	"encoding/json"

	"github.com/breez/lntest"
	"github.com/breez/lspd/lsps0"
	"github.com/stretchr/testify/assert"
)

func testLsps0GetProtocolVersions(p *testParams) {
	p.BreezClient().Node().ConnectPeer(p.Lsp().LightningNode())

	rawMsg := `{
		"method": "lsps0.list_protocols",
		"jsonrpc": "2.0",
		"id": "example#3cad6a54d302edba4c9ade2f7ffac098",
		"params": {}
	  }`
	p.BreezClient().Node().SendCustomMessage(&lntest.CustomMsgRequest{
		PeerId: hex.EncodeToString(p.Lsp().NodeId()),
		Type:   lsps0.Lsps0MessageType,
		Data:   []byte(rawMsg),
	})

	resp := p.BreezClient().ReceiveCustomMessage()
	assert.Equal(p.t, uint32(37913), resp.Type)

	content := make(map[string]interface{})
	err := json.Unmarshal(resp.Data[:], &content)
	lntest.CheckError(p.t, err)

	assert.Equal(p.t, "2.0", content["jsonrpc"])
	assert.Equal(p.t, "example#3cad6a54d302edba4c9ade2f7ffac098", content["id"])

	content2 := make(map[string]json.RawMessage)
	err = json.Unmarshal(resp.Data[:], &content2)
	lntest.CheckError(p.t, err)

	result := make(map[string][]int)
	err = json.Unmarshal(content2["result"], &result)
	lntest.CheckError(p.t, err)
	assert.Equal(p.t, []int{2}, result["protocols"])
}

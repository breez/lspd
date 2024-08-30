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

func testLsps2GetVersions(p *testParams) {
	p.BreezClient().Node().ConnectPeer(p.Lsp().LightningNode())

	// Make sure everything is activated.
	<-time.After(htlcInterceptorDelay)

	rawMsg := `{
		"method": "lsps2.get_versions",
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

	content := make(map[string]json.RawMessage)
	err := json.Unmarshal(resp.Data[:], &content)
	lntest.CheckError(p.t, err)

	log.Print(string(resp.Data))
	result := make(map[string][]int)
	err = json.Unmarshal(content["result"], &result)
	lntest.CheckError(p.t, err)
	assert.Equal(p.t, []int{1}, result["versions"])
}

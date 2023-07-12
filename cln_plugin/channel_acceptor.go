package cln_plugin

import (
	"encoding/json"
	"fmt"

	sj "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
)

func channelAcceptor(acceptScript string, method string, openChannel json.RawMessage) (json.RawMessage, error) {
	reject, _ := json.Marshal(struct {
		Result string `json:"result"`
	}{Result: "reject"})
	accept, _ := json.Marshal(struct {
		Result string `json:"result"`
	}{Result: "continue"})

	if acceptScript == "" {
		return accept, nil
	}

	sd := starlark.StringDict{
		"method":      starlark.String(method),
		"openchannel": starlark.String(openChannel),
	}
	for _, k := range sj.Module.Members.Keys() {
		sd[k] = sj.Module.Members[k]
	}
	value, err := starlark.Eval(
		&starlark.Thread{},
		"",
		acceptScript,
		sd,
	)
	if err != nil {
		return reject, err
	}
	s, ok := value.(starlark.String)
	if !ok {
		return reject, fmt.Errorf("not a string")
	}
	return json.RawMessage(s.GoString()), nil
}

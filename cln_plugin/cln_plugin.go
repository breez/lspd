// The code in this plugin is highly inspired by and sometimes copied from
// github.com/niftynei/glightning. Therefore pieces of this code are subject
// to Copyright Lisa Neigut (Blockstream) 2019.

package cln_plugin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	SubscriberTimeoutOption = "lsp-subscribertimeout"
	ListenAddressOption     = "lsp-listen"
	channelAcceptScript     = "lsp-channel-accept-script"
)

var (
	DefaultSubscriberTimeout     = "1m"
	DefaultChannelAcceptorScript = ""
	LspsFeatureBit               = "0200000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
)

const (
	MaxIntakeBuffer = 500 * 1024 * 1023
)

const (
	SpecVersion    = "2.0"
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32603
	InternalErr    = -32603
)

var (
	TwoNewLines = []byte("\n\n")
)

type ClnPlugin struct {
	done                chan struct{}
	server              *server
	in                  *os.File
	out                 *bufio.Writer
	writeMtx            sync.Mutex
	channelAcceptScript string
}

func NewClnPlugin(in, out *os.File) *ClnPlugin {
	c := &ClnPlugin{
		done: make(chan struct{}),
		in:   in,
		out:  bufio.NewWriter(out),
	}

	return c
}

// Starts the cln plugin.
// NOTE: The grpc server is started in the handleInit function.
func (c *ClnPlugin) Start() {
	c.setupLogging()
	go c.listenRequests()
	<-c.done
	s := c.server
	if s != nil {
		<-s.completed
	}
}

// Stops the cln plugin. Drops any remaining work immediately.
// Pending htlcs will be replayed when cln starts again.
func (c *ClnPlugin) Stop() {
	log.Printf("Stop called. Stopping plugin.")
	close(c.done)

	s := c.server
	if s != nil {
		s.Stop()
	}
}

// listens stdout for requests from cln and sends the requests to the
// appropriate handler in fifo order.
func (c *ClnPlugin) listenRequests() error {
	scanner := bufio.NewScanner(c.in)
	buf := make([]byte, 1024)
	scanner.Buffer(buf, MaxIntakeBuffer)

	// cln messages are split by a double newline.
	scanner.Split(scanDoubleNewline)
	for {
		select {
		case <-c.done:
			return nil
		default:
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					log.Fatal(err)
					return err
				}

				return nil
			}

			msg := scanner.Bytes()

			// Always log the message json
			log.Println(string(msg))

			// pass down a copy so things stay sane
			msg_buf := make([]byte, len(msg))
			copy(msg_buf, msg)

			// NOTE: processMsg is synchronous, so it should only do quick work.
			c.processMsg(msg_buf)
		}
	}
}

// Listens to responses to htlc_accepted requests from the grpc server.
func (c *ClnPlugin) htlcListenServer() {
	for {
		select {
		case <-c.done:
			return
		default:
			id, result := c.server.ReceiveHtlcResolution()

			// The server may return nil if it is stopped.
			if result == nil {
				continue
			}

			serid, _ := json.Marshal(&id)
			c.sendToCln(&Response{
				Id:      serid,
				JsonRpc: SpecVersion,
				Result:  result,
			})
		}
	}
}

// Listens to responses to custommsg requests from the grpc server.
func (c *ClnPlugin) custommsgListenServer() {
	for {
		select {
		case <-c.done:
			return
		default:
			id, result := c.server.ReceiveCustomMessageResponse()

			// The server may return nil if it is stopped.
			if result == nil {
				continue
			}

			serid, _ := json.Marshal(&id)
			c.sendToCln(&Response{
				Id:      serid,
				JsonRpc: SpecVersion,
				Result:  result,
			})
		}
	}
}

// processes a single message from cln. Sends the message to the appropriate
// handler.
func (c *ClnPlugin) processMsg(msg []byte) {
	if len(msg) == 0 {
		c.sendError(nil, InvalidRequest, "Got an invalid zero length request")
		return
	}

	// Handle request batches.
	if msg[0] == '[' {
		var requests []*Request
		err := json.Unmarshal(msg, &requests)
		if err != nil {
			c.sendError(
				nil,
				ParseError,
				fmt.Sprintf("Failed to unmarshal request batch: %v", err),
			)
			return
		}

		for _, request := range requests {
			c.processRequest(request)
		}

		return
	}

	// Parse the received buffer into a request object.
	var request Request
	err := json.Unmarshal(msg, &request)
	if err != nil {
		c.sendError(
			nil,
			ParseError,
			fmt.Sprintf("failed to unmarshal request: %v", err),
		)
		return
	}

	c.processRequest(&request)
}

func (c *ClnPlugin) processRequest(request *Request) {
	// Make sure the spec version is expected.
	if request.JsonRpc != SpecVersion {
		c.sendError(request.Id, InvalidRequest, fmt.Sprintf(
			`Invalid jsonrpc, expected '%s' got '%s'`,
			SpecVersion,
			request.JsonRpc,
		))
		return
	}

	// Send the message to the appropriate handler.
	switch request.Method {
	case "getmanifest":
		c.handleGetManifest(request)
	case "init":
		c.handleInit(request)
	case "shutdown":
		c.handleShutdown(request)
	case "htlc_accepted":
		c.handleHtlcAccepted(request)
	case "openchannel":
		// handle open channel in a goroutine, because order doesn't  matter.
		go c.handleOpenChannel(request)
	case "openchannel2":
		// handle open channel in a goroutine, because order doesn't  matter.
		go c.handleOpenChannel(request)
	case "getchannelacceptscript":
		c.sendToCln(&Response{
			JsonRpc: SpecVersion,
			Id:      request.Id,
			Result:  c.channelAcceptScript,
		})
	case "setchannelacceptscript":
		c.handleSetChannelAcceptScript(request)
	case "custommsg":
		c.handleCustomMsg(request)
	default:
		c.sendError(
			request.Id,
			MethodNotFound,
			fmt.Sprintf("Method '%s' not found", request.Method),
		)
	}
}

// Returns this plugin's manifest to cln.
func (c *ClnPlugin) handleGetManifest(request *Request) {
	c.sendToCln(&Response{
		Id:      request.Id,
		JsonRpc: SpecVersion,
		Result: &Manifest{
			Options: []Option{
				{
					Name: ListenAddressOption,
					Type: "string",
					Description: "listen address for the htlc_accepted lsp " +
						"grpc server",
				},
				{
					Name: SubscriberTimeoutOption,
					Type: "string",
					Description: "the maximum duration we will hold a htlc " +
						"if no subscriber is active. golang duration string.",
					Default: &DefaultSubscriberTimeout,
				},
				{
					Name:        channelAcceptScript,
					Type:        "string",
					Description: "starlark script for channel acceptor.",
					Default:     &DefaultChannelAcceptorScript,
				},
			},
			RpcMethods: []*RpcMethod{
				{
					Name:        "getchannelacceptscript",
					Description: "Get the startlark channel acceptor script",
				},
				{
					Name:        "setchannelacceptscript",
					Description: "Set the startlark channel acceptor script",
				},
			},
			Dynamic: false,
			Hooks: []Hook{
				{Name: "custommsg"},
				{Name: "htlc_accepted"},
				{Name: "openchannel"},
				{Name: "openchannel2"},
			},
			NonNumericIds: true,
			Subscriptions: []string{
				"shutdown",
			},
			FeatureBits: &FeatureBits{
				Node: &LspsFeatureBit,
			},
		},
	})
}

// Handles plugin initialization. Parses startup options and starts the grpc
// server.
func (c *ClnPlugin) handleInit(request *Request) {
	// Deserialize the init message.
	var initMsg InitMessage
	err := json.Unmarshal(request.Params, &initMsg)
	if err != nil {
		c.sendError(
			request.Id,
			ParseError,
			fmt.Sprintf("Failed to unmarshal init params: %v", err),
		)
		return
	}

	// Get the channel acceptor script option.
	sc, ok := initMsg.Options[channelAcceptScript]
	if !ok {
		c.sendError(
			request.Id,
			InvalidParams,
			fmt.Sprintf("Missing option '%s'", channelAcceptScript),
		)
		return
	}

	c.channelAcceptScript, ok = sc.(string)
	if !ok {
		c.sendError(
			request.Id,
			InvalidParams,
			fmt.Sprintf(
				"Invalid value '%v' for option '%s'",
				sc,
				channelAcceptScript,
			),
		)
		return
	}

	// Get the listen address option.
	l, ok := initMsg.Options[ListenAddressOption]
	if !ok {
		c.sendError(
			request.Id,
			InvalidParams,
			fmt.Sprintf("Missing option '%s'", ListenAddressOption),
		)
		return
	}

	addr, ok := l.(string)
	if !ok || addr == "" {
		c.sendError(
			request.Id,
			InvalidParams,
			fmt.Sprintf(
				"Invalid value '%v' for option '%s'",
				l,
				ListenAddressOption,
			),
		)
		return
	}

	// Get the subscriber timeout option.
	t, ok := initMsg.Options[SubscriberTimeoutOption]
	if !ok {
		c.sendError(
			request.Id,
			InvalidParams,
			fmt.Sprintf("Missing option '%s'", SubscriberTimeoutOption),
		)
		return
	}

	s, ok := t.(string)
	if !ok || s == "" {
		c.sendError(
			request.Id,
			InvalidParams,
			fmt.Sprintf(
				"Invalid value '%v' for option '%s'",
				t,
				SubscriberTimeoutOption,
			),
		)
		return
	}

	subscriberTimeout, err := time.ParseDuration(s)
	if err != nil {
		c.sendError(
			request.Id,
			InvalidParams,
			fmt.Sprintf(
				"Invalid value '%v' for option '%s'",
				s,
				SubscriberTimeoutOption,
			),
		)
		return
	}

	// Start the grpc server.
	c.server = NewServer(addr, subscriberTimeout)
	go c.server.Start()
	err = c.server.WaitStarted()
	if err != nil {
		c.sendError(
			request.Id,
			InternalErr,
			fmt.Sprintf("Failed to start server: %s", err.Error()),
		)
		return
	}

	// Listen for htlc and custommsg responses from the grpc server.
	go c.htlcListenServer()
	go c.custommsgListenServer()

	// Let cln know the plugin is initialized.
	c.sendToCln(&Response{
		Id:      request.Id,
		JsonRpc: SpecVersion,
	})
}

// Handles the shutdown message. Stops any work immediately.
func (c *ClnPlugin) handleShutdown(request *Request) {
	c.Stop()
}

// Sends a htlc_accepted message to the grpc server.
func (c *ClnPlugin) handleHtlcAccepted(request *Request) {
	var htlc HtlcAccepted
	err := json.Unmarshal(request.Params, &htlc)
	if err != nil {
		c.sendError(
			request.Id,
			ParseError,
			fmt.Sprintf(
				"Failed to unmarshal htlc_accepted params:%s [%s]",
				err.Error(),
				request.Params,
			),
		)
		return
	}

	c.server.SendHtlcAccepted(idToString(request.Id), &htlc)
}

func (c *ClnPlugin) handleSetChannelAcceptScript(request *Request) {
	var params []string
	err := json.Unmarshal(request.Params, &params)
	if err != nil {
		c.sendError(
			request.Id,
			ParseError,
			fmt.Sprintf(
				"Failed to unmarshal setchannelacceptscript params:%s [%s]",
				err.Error(),
				request.Params,
			),
		)
		return
	}
	if len(params) >= 1 {
		c.channelAcceptScript = params[0]
	}
	c.sendToCln(&Response{
		JsonRpc: SpecVersion,
		Id:      request.Id,
		Result:  c.channelAcceptScript,
	})
}

func (c *ClnPlugin) handleCustomMsg(request *Request) {
	var custommsg CustomMessageRequest
	err := json.Unmarshal(request.Params, &custommsg)
	if err != nil {
		c.sendError(
			request.Id,
			ParseError,
			fmt.Sprintf(
				"Failed to unmarshal custommsg params:%s [%s]",
				err.Error(),
				request.Params,
			),
		)
		return
	}

	c.server.SendCustomMessage(idToString(request.Id), &custommsg)
}

func unmarshalOpenChannel(request *Request) (r json.RawMessage, err error) {
	switch request.Method {
	case "openchannel":
		var openChannel struct {
			OpenChannel json.RawMessage `json:"openchannel"`
		}
		err = json.Unmarshal(request.Params, &openChannel)
		if err != nil {
			return
		}
		r = openChannel.OpenChannel
	case "openchannel2":
		var openChannel struct {
			OpenChannel json.RawMessage `json:"openchannel2"`
		}
		err = json.Unmarshal(request.Params, &openChannel)
		if err != nil {
			return
		}
		r = openChannel.OpenChannel
	}
	return r, nil
}
func (c *ClnPlugin) handleOpenChannel(request *Request) {
	p, err := unmarshalOpenChannel(request)
	if err != nil {
		c.sendError(
			request.Id,
			ParseError,
			fmt.Sprintf(
				"Failed to unmarshal openchannel params:%s [%s]",
				err.Error(),
				request.Params,
			),
		)
		return
	}
	result, err := channelAcceptor(c.channelAcceptScript, request.Method, p)
	if err != nil {
		log.Printf("channelAcceptor error - request: %s error: %v", request, err)
	}
	c.sendToCln(&Response{
		JsonRpc: SpecVersion,
		Id:      request.Id,
		Result:  result,
	})
}

// Sends an error to cln.
func (c *ClnPlugin) sendError(id json.RawMessage, code int, message string) {
	// Log the error to cln first.
	c.log("error", message)

	// Then create an error message.
	resp := &Response{
		JsonRpc: SpecVersion,
		Error: &RpcError{
			Code:    code,
			Message: message,
		},
	}

	if len(id) > 0 {
		resp.Id = id
	}

	c.sendToCln(resp)
}

// Sends a message to cln.
func (c *ClnPlugin) sendToCln(msg interface{}) {
	c.writeMtx.Lock()
	defer c.writeMtx.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message for cln, ignoring message: %+v", msg)
		return
	}

	data = append(data, TwoNewLines...)
	c.out.Write(data)
	c.out.Flush()
}

func (c *ClnPlugin) setupLogging() {
	in, out := io.Pipe()
	log.SetFlags(log.Ltime | log.Lshortfile)
	log.SetOutput(out)
	go func(in io.Reader) {
		// everytime we get a new message, log it thru c-lightning
		scanner := bufio.NewScanner(in)
		for {
			select {
			case <-c.done:
				return
			default:
				if !scanner.Scan() {
					if err := scanner.Err(); err != nil {
						log.Fatalf(
							"can't print out to std err, killing: %v",
							err,
						)
					}
				}

				for _, line := range strings.Split(scanner.Text(), "\n") {
					c.log("info", line)
				}
			}
		}

	}(in)
}

func (c *ClnPlugin) log(level string, message string) {
	params, _ := json.Marshal(&LogNotification{
		Level:   level,
		Message: message,
	})

	c.sendToCln(&Request{
		Method:  "log",
		JsonRpc: SpecVersion,
		Params:  params,
	})
}

// Helper method for the bufio scanner to split messages on double newlines.
func scanDoubleNewline(
	data []byte,
	atEOF bool,
) (advance int, token []byte, err error) {
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' && (i+1) < len(data) && data[i+1] == '\n' {
			return i + 2, data[:i], nil
		}
	}
	// this trashes anything left over in
	// the buffer if we're at EOF, with no /n/n present
	return 0, nil, nil
}

// converts a raw cln id to string. The CLN id can either be an integer or a
// string. if it's a string, the quotes are removed.
func idToString(id json.RawMessage) string {
	if len(id) == 0 {
		return ""
	}

	str := string(id)
	str = strings.TrimSpace(str)
	str = strings.Trim(str, "\"")
	str = strings.Trim(str, "'")
	return str
}

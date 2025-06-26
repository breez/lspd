// The code in this plugin is highly inspired by and sometimes copied from
// github.com/niftynei/glightning. Therefore pieces of this code are subject
// to Copyright Lisa Neigut (Blockstream) 2019.

package cln_plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/breez/lspd/build"
)

const (
	SubscriberTimeoutOption = "lsp-subscribertimeout"
	ListenAddressOption     = "lsp-listen"
	channelAcceptScript     = "lsp-channel-accept-script"
)

var (
	DefaultSubscriberTimeout     = "1m"
	DefaultChannelAcceptorScript = ""
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
	ctx                 context.Context
	cancel              context.CancelFunc
	server              *server
	serverStopped       sync.WaitGroup
	reader              *reader
	writer              *writer
	stderr              *os.File
	channelAcceptScript string
}

func NewClnPlugin(stdin, stdout, stderr *os.File) *ClnPlugin {
	ctx, cancel := context.WithCancel(context.Background())
	reader := newReader(stdin)
	writer := newWriter(stdout)
	c := &ClnPlugin{
		ctx:    ctx,
		cancel: cancel,
		reader: reader,
		writer: writer,
		stderr: stderr,
		server: NewServer(),
	}

	return c
}

// Starts the cln plugin. This function should only be called once.
// NOTE: The grpc server is started in the handleInit function.
func (c *ClnPlugin) Start() error {
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}

	c.setupClnLogging()
	log.Printf(`Starting lspd cln_plugin, tag='%s', revision='%s'`, build.GetTag(), build.GetRevision())
	go func() {
		err := c.listenRequests()
		if err != nil {
			log.Printf("listenRequests exited with error: %v", err)
			c.Stop()
		}
	}()
	<-c.ctx.Done()
	c.serverStopped.Wait()

	log.Printf("lspd cln_plugin stopped.")
	return c.ctx.Err()
}

// Stops the cln plugin. Drops any remaining work immediately.
// Pending htlcs will be replayed when cln starts again.
func (c *ClnPlugin) Stop() {
	log.Printf("Stop called. Stopping plugin.")
	c.cancel()
}

// listens stdout for requests from cln and sends the requests to the
// appropriate handler in fifo order.
func (c *ClnPlugin) listenRequests() error {
	go func() {
		<-c.ctx.Done()

		// Close the reader (closes stdin internally) when cancelled, so the
		// blocking read call is released.
		c.reader.Close()
	}()

	for {
		select {
		case <-c.ctx.Done():
			return nil
		default:
			req, err := c.reader.Next()
			if err != nil {
				log.Printf("Failed to read next request: %v", err)
				return err
			}

			c.processRequest(req)
		}
	}
}

// Listens to responses to htlc_accepted requests from the grpc server.
func (c *ClnPlugin) htlcListenServer() {
	for {
		select {
		case <-c.ctx.Done():
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
				{Name: "htlc_accepted"},
				{Name: "openchannel"},
				{Name: "openchannel2"},
			},
			NonNumericIds: true,
			Subscriptions: []string{
				"shutdown",
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

	c.serverStopped.Add(1)
	go func() {
		<-c.ctx.Done()
		c.server.Stop()
	}()
	go func() {
		// Start the grpc server.
		err := c.server.Start(addr, subscriberTimeout)
		if err != nil {
			log.Printf("server exited with error: %v", err)
		}
		c.serverStopped.Done()
		c.Stop()
	}()
	err = c.server.WaitStarted()
	if err != nil {
		c.sendError(
			request.Id,
			InternalErr,
			fmt.Sprintf("Failed to start server: %s", err.Error()),
		)
		return
	}

	// Listen for htlc responses from the grpc server.
	go c.htlcListenServer()

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
func (c *ClnPlugin) sendToCln(msg interface{}) error {
	err := c.writer.Write(msg)
	if err != nil {
		log.Printf("Failed to send message to cln: %v", err)
		c.Stop()
		return fmt.Errorf("failed to send message to cln: %w", err)
	}

	return nil
}

func (c *ClnPlugin) setupClnLogging() {
	in, out := io.Pipe()
	log.SetOutput(out)
	errLogger := log.New(c.stderr, log.Prefix(), log.Ltime|log.Lshortfile)
	go func(in io.Reader) {
		// everytime we get a new message, log it through cln
		scanner := bufio.NewScanner(in)
		for {
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					errLogger.Printf("Log input stream died: %s", err.Error())
					<-time.After(time.Second)
				}

				continue
			}

			for _, line := range strings.Split(scanner.Text(), "\n") {
				err := c.log("info", line)
				if err != nil {
					errLogger.Printf("Failed to write log '%s' to cln, error: %s", line, err.Error())
				}
			}
		}
	}(in)
}

func (c *ClnPlugin) log(level string, message string) error {
	params, _ := json.Marshal(&LogNotification{
		Level:   level,
		Message: message,
	})

	return c.sendToCln(&Request{
		Method:  "log",
		JsonRpc: SpecVersion,
		Params:  params,
	})
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

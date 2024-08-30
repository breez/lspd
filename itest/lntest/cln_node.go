package lntest

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/breez/lspd/cln/rpc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/exp/slices"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type ClnNode struct {
	name           string
	binary         string
	args           []string
	regtestDir     string
	logfilePath    string
	nodeId         []byte
	harness        *TestHarness
	miner          *Miner
	dir            string
	host           string
	port           uint32
	grpcHost       string
	grpcPort       uint32
	caCertPath     string
	clientCertPath string
	clientKeyPath  string
	privkey        *secp256k1.PrivateKey
	runtime        *clnNodeRuntime
	mtx            sync.Mutex
	timesStarted   int
	socketDir      string
	socketFile     string
}

type clnNodeRuntime struct {
	conn     *grpc.ClientConn
	rpc      rpc.NodeClient
	cmd      *exec.Cmd
	cleanups []*Cleanup
	done     chan struct{}
}

func NewClnNode(h *TestHarness, m *Miner, name string, extraArgs ...string) *ClnNode {
	binary, err := GetLightningdBinary()
	CheckError(h.T, err)

	return NewClnNodeFromBinary(h, m, name, binary, extraArgs...)
}

func NewClnNodeFromBinary(h *TestHarness, m *Miner, name string, binary string, extraArgs ...string) *ClnNode {
	lightningdDir := h.GetDirectory(fmt.Sprintf("ld-%s", name))
	regtestDir := filepath.Join(lightningdDir, "regtest")
	logfilePath := filepath.Join(regtestDir, "log")
	host := "localhost"
	port, err := GetPort()
	CheckError(h.T, err)

	grpcPort, err := GetPort()
	CheckError(h.T, err)

	bitcoinCliBinary, err := GetBitcoinCliBinary()
	CheckError(h.T, err)

	privKey, err := btcec.NewPrivateKey()
	CheckError(h.T, err)

	s := privKey.Serialize()
	p := privKey.PubKey().SerializeCompressed()
	args := append([]string{
		"--network=regtest",
		"--log-file=log",
		"--log-level=debug",
		"--bitcoin-rpcuser=btcuser",
		"--bitcoin-rpcpassword=btcpass",
		"--dev-bitcoind-poll=1",
		"--dev-fast-gossip",
		"--dev-fast-reconnect",
		"--developer",

		fmt.Sprintf("--dev-force-privkey=%x", s),
		fmt.Sprintf("--lightning-dir=%s", lightningdDir),
		fmt.Sprintf("--bitcoin-datadir=%s", m.dir),
		fmt.Sprintf("--addr=%s:%d", host, port),
		fmt.Sprintf("--grpc-port=%d", grpcPort),
		fmt.Sprintf("--bitcoin-rpcport=%d", m.rpcPort),
		fmt.Sprintf("--bitcoin-cli=%s", bitcoinCliBinary),
	}, extraArgs...)

	node := &ClnNode{
		name:           name,
		binary:         binary,
		args:           args,
		regtestDir:     regtestDir,
		logfilePath:    logfilePath,
		nodeId:         p,
		harness:        h,
		miner:          m,
		dir:            lightningdDir,
		port:           port,
		host:           host,
		grpcHost:       host,
		grpcPort:       grpcPort,
		caCertPath:     filepath.Join(regtestDir, "ca.pem"),
		clientCertPath: filepath.Join(regtestDir, "client.pem"),
		clientKeyPath:  filepath.Join(regtestDir, "client-key.pem"),
		privkey:        privKey,
		socketDir:      regtestDir,
		socketFile:     "lightning-rpc",
	}

	h.AddStoppable(node)
	h.RegisterLogfile(filepath.Join(regtestDir, "log"), fmt.Sprintf("lightningd-%s", name))

	return node
}

func (n *ClnNode) Start() {
	n.mtx.Lock()
	defer n.mtx.Unlock()

	if n.runtime != nil {
		log.Printf("%s: Start called, but was already started.", n.name)
		return
	}

	var cleanups []*Cleanup
	cmd := exec.CommandContext(n.harness.Ctx, n.binary, n.args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stderr, err := cmd.StderrPipe()
	CheckError(n.harness.T, err)

	stdout, err := cmd.StdoutPipe()
	CheckError(n.harness.T, err)

	log.Printf("%s: starting %s on port %d in dir %s...", n.name, n.binary, n.port, n.dir)
	err = cmd.Start()
	CheckError(n.harness.T, err)

	done := make(chan struct{})
	cleanups = append(cleanups, &Cleanup{
		Name: "cmd",
		Fn: func() error {
			proc := cmd.Process
			if proc != nil {
				sig := syscall.SIGINT
				if runtime.GOOS == "windows" {
					sig = syscall.SIGKILL
				}

				return syscall.Kill(-proc.Pid, sig)
			}

			<-done
			return nil
		},
	})

	go func() {
		// print any stderr output to the test log
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Println(n.name + ": " + scanner.Text())
		}
	}()

	go func() {
		// print any stderr output to the test log
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			log.Println(n.name + ": " + scanner.Text())
		}
	}()

	go func() {
		err := cmd.Wait()
		if err != nil && err.Error() != "signal: interrupt" {
			log.Printf("%s: lightningd exited with error %s", n.name, err)
		} else {
			log.Printf("%s: process exited normally", n.name)
		}
		close(done)
	}()

	err = n.waitForLog("Server started with public key")
	if err != nil {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: Error waiting for cln to start: %v", n.name, err)
	}
	n.timesStarted += 1

	pemServerCA, err := os.ReadFile(n.caCertPath)
	if err != nil {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: Failed to read ca.pem: %v", n.name, err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemServerCA) {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: failed to add server CA's certificate", n.name)
	}

	clientCert, err := tls.LoadX509KeyPair(
		n.clientCertPath,
		n.clientKeyPath,
	)
	if err != nil {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: Failed to load LoadX509KeyPair: %v", n.name, err)
	}

	tlsConfig := &tls.Config{
		RootCAs:      certPool,
		Certificates: []tls.Certificate{clientCert},
	}

	tlsCredentials := credentials.NewTLS(tlsConfig)

	grpcAddress := fmt.Sprintf("%s:%d", n.host, n.grpcPort)
	conn, err := grpc.Dial(
		grpcAddress,
		grpc.WithTransportCredentials(tlsCredentials),
	)
	if err != nil {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: Failed to obtain grpc client: %v", n.name, err)
	}
	cleanups = append(cleanups, &Cleanup{
		Name: "grpc conn",
		Fn:   conn.Close,
	})

	client := rpc.NewNodeClient(conn)
	info, err := client.Getinfo(n.harness.Ctx, &rpc.GetinfoRequest{})
	if err != nil {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: Failed to call Getinfo: %v", n.name, err)
	}
	log.Printf("%s: Has node id %x", n.name, info.Id)

	n.runtime = &clnNodeRuntime{
		conn:     conn,
		rpc:      client,
		cmd:      cmd,
		cleanups: cleanups,
		done:     done,
	}
}

func (n *ClnNode) Stop() error {
	n.mtx.Lock()
	defer n.mtx.Unlock()

	if n.runtime == nil {
		log.Printf("%s: Stop called, but was already stopped.", n.name)
		return nil
	}

	PerformCleanup(n.runtime.cleanups)
	n.runtime = nil
	return nil
}

func (n *ClnNode) waitForLog(phrase string) error {
	logfilePath := filepath.Join(n.dir, "regtest", "log")
	// at startup we need to wait for the file to open
	for time.Now().Before(n.harness.Deadline()) {
		if _, err := os.Stat(logfilePath); os.IsNotExist(err) {
			<-time.After(waitSleepInterval)
			continue
		}
		break
	}
	logfile, _ := os.Open(logfilePath)
	defer logfile.Close()

	reader := bufio.NewReader(logfile)
	counted := 0
	for time.Now().Before(n.harness.Deadline()) {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				<-time.After(waitSleepInterval)
			} else {
				return err
			}
		}
		m, err := regexp.MatchString(phrase, line)
		if err != nil {
			return err
		}

		if m {
			if counted == n.timesStarted {
				return nil
			}

			counted += 1
		}
	}

	return fmt.Errorf("unable to find \"%s\" in %s", phrase, logfilePath)
}

func (n *ClnNode) NodeId() []byte {
	return n.nodeId
}

func (n *ClnNode) Host() string {
	return n.host
}

func (n *ClnNode) Port() uint32 {
	return n.port
}

func (n *ClnNode) PrivateKey() *secp256k1.PrivateKey {
	return n.privkey
}

func (n *ClnNode) IsStarted() bool {
	return n.runtime != nil
}

func (n *ClnNode) SocketDir() string {
	return n.socketDir
}

func (n *ClnNode) SocketFile() string {
	return n.socketFile
}

func (n *ClnNode) GrpcPort() uint32 {
	return n.grpcPort
}

func (n *ClnNode) CaCertPath() string {
	return n.caCertPath
}

func (n *ClnNode) ClientCertPath() string {
	return n.clientCertPath
}

func (n *ClnNode) ClientKeyPath() string {
	return n.clientKeyPath
}

func (n *ClnNode) WaitForSync() {
	for {
		info, _ := n.runtime.rpc.Getinfo(n.harness.Ctx, &rpc.GetinfoRequest{})

		blockHeight := n.miner.GetBlockHeight()

		if (info.WarningLightningdSync == nil || *info.WarningLightningdSync == "") &&
			info.Blockheight >= blockHeight {
			log.Printf("%s: Synced to blockheight %d", n.name, blockHeight)
			break
		}

		log.Printf(
			"%s: Waiting to sync. Actual block height: %d, node block height: %d",
			n.name,
			blockHeight,
			info.Blockheight,
		)

		if time.Now().After(n.harness.Deadline()) {
			n.harness.T.Fatal("timed out waiting for channel normal")
		}

		<-time.After(waitSleepInterval)
	}
}

func (n *ClnNode) GetNewAddress() string {
	addrResponse, err := n.runtime.rpc.NewAddr(
		n.harness.Ctx,
		&rpc.NewaddrRequest{
			Addresstype: rpc.NewaddrRequest_BECH32.Enum(),
		},
	)
	CheckError(n.harness.T, err)
	return *addrResponse.Bech32
}

func (n *ClnNode) SendToAddress(addr string, amountSat uint64) {
	prep, err := n.runtime.rpc.TxPrepare(n.harness.Ctx, &rpc.TxprepareRequest{
		Outputs: []*rpc.OutputDesc{
			{
				Address: addr,
				Amount: &rpc.Amount{
					Msat: amountSat * 1000,
				},
			},
		},
	})
	CheckError(n.harness.T, err)

	_, err = n.runtime.rpc.TxSend(n.harness.Ctx, &rpc.TxsendRequest{
		Txid: prep.Txid,
	})
	CheckError(n.harness.T, err)
}

func (n *ClnNode) Fund(amountSat uint64) {
	addr := n.GetNewAddress()
	n.miner.SendToAddressAndMine(addr, amountSat, 1)
	n.WaitForSync()
}

func (n *ClnNode) ConnectPeer(peer LightningNode) {
	host := peer.Host()
	port := peer.Port()
	_, err := n.runtime.rpc.ConnectPeer(n.harness.Ctx, &rpc.ConnectRequest{
		Id:   hex.EncodeToString(peer.NodeId()),
		Host: &host,
		Port: &port,
	})
	CheckError(n.harness.T, err)
}

func (n *ClnNode) OpenChannel(peer LightningNode, options *OpenChannelOptions) *ChannelInfo {
	n.ConnectPeer(peer)

	// open a channel
	fundResult, err := n.runtime.rpc.FundChannel(n.harness.Ctx, &rpc.FundchannelRequest{
		Id: peer.NodeId(),
		Amount: &rpc.AmountOrAll{
			Value: &rpc.AmountOrAll_Amount{
				Amount: &rpc.Amount{
					Msat: options.AmountSat * 1000,
				},
			},
		},
		Announce: &options.IsPublic,
	})
	CheckError(n.harness.T, err)

	txid := reverseBytes(fundResult.Txid[:])
	return &ChannelInfo{
		From:            n,
		To:              peer,
		FundingTxId:     txid,
		FundingTxOutnum: fundResult.Outnum,
	}
}

func (n *ClnNode) WaitForChannelReady(channel *ChannelInfo) ShortChannelID {
	log.Printf("%s: Wait for channel ready.", n.name)
	peerId := channel.GetPeer(n).NodeId()

	for {
		resp, err := n.runtime.rpc.ListPeerChannels(n.harness.Ctx, &rpc.ListpeerchannelsRequest{
			Id: peerId,
		})
		CheckError(n.harness.T, err)
		if resp.Channels == nil {
			n.harness.T.Fatal("no channels for peer")
		}

		channelTxId := reverseBytes(channel.FundingTxId[:])
		channelIndex := slices.IndexFunc(
			resp.Channels,
			func(pc *rpc.ListpeerchannelsChannels) bool {
				return bytes.Equal(pc.FundingTxid, channelTxId) &&
					*pc.FundingOutnum == channel.FundingTxOutnum
			},
		)
		if channelIndex >= 0 {
			peerChannel := resp.Channels[channelIndex]
			if *peerChannel.State == rpc.ListpeerchannelsChannels_CHANNELD_AWAITING_LOCKIN {
				log.Printf("%s: Channel state is CHANNELD_AWAITING_LOCKIN, mining some blocks.", n.name)
				n.miner.MineBlocks(6)
				n.WaitForSync()
				continue
			}

			return NewShortChanIDFromString(*peerChannel.ShortChannelId)
		}

		if time.Now().After(n.harness.Deadline()) {
			n.harness.T.Fatal("timed out waiting for channel normal")
		}

		<-time.After(waitSleepInterval)
	}
}

func (n *ClnNode) CreateBolt11Invoice(options *CreateInvoiceOptions) *CreateInvoiceResult {
	label, err := GenerateRandomString()
	CheckError(n.harness.T, err)

	req := &rpc.InvoiceRequest{
		Label: label,
		AmountMsat: &rpc.AmountOrAny{
			Value: &rpc.AmountOrAny_Amount{
				Amount: &rpc.Amount{
					Msat: options.AmountMsat,
				},
			},
		},
	}

	if options.Description != nil {
		req.Description = *options.Description
	}

	if options.Preimage != nil {
		req.Preimage = *options.Preimage
	}

	if options.Cltv != nil {
		req.Cltv = options.Cltv
	}

	resp, err := n.runtime.rpc.Invoice(n.harness.Ctx, req)
	CheckError(n.harness.T, err)

	return &CreateInvoiceResult{
		Bolt11:        resp.Bolt11,
		PaymentHash:   resp.PaymentHash,
		PaymentSecret: resp.PaymentSecret,
	}
}

func (n *ClnNode) SignMessage(message []byte) []byte {
	resp, err := n.runtime.rpc.SignMessage(n.harness.Ctx, &rpc.SignmessageRequest{
		Message: hex.EncodeToString(message),
	})
	CheckError(n.harness.T, err)

	return resp.Signature
}

func (n *ClnNode) Pay(bolt11 string) *PayResult {
	rpcTimeout := getTimeoutSeconds(n.harness.T, n.harness.Deadline())
	resp, err := n.runtime.rpc.Pay(n.harness.Ctx, &rpc.PayRequest{
		Bolt11:   bolt11,
		RetryFor: &rpcTimeout,
	})
	CheckError(n.harness.T, err)

	return &PayResult{
		PaymentHash:     resp.PaymentHash,
		AmountMsat:      resp.AmountMsat.Msat,
		Destination:     resp.Destination,
		AmountSentMsat:  resp.AmountSentMsat.Msat,
		PaymentPreimage: resp.PaymentPreimage,
	}
}

func (n *ClnNode) GetRoute(destination []byte, amountMsat uint64) *Route {
	route, err := n.runtime.rpc.GetRoute(n.harness.Ctx, &rpc.GetrouteRequest{
		Id: destination,
		AmountMsat: &rpc.Amount{
			Msat: amountMsat,
		},
		Riskfactor: 0,
	})
	CheckError(n.harness.T, err)

	result := &Route{}
	for _, hop := range route.Route {
		result.Hops = append(result.Hops, &Hop{
			Id:         hop.Id,
			Channel:    NewShortChanIDFromString(hop.Channel),
			AmountMsat: hop.AmountMsat.Msat,
			Delay:      uint16(hop.Delay),
		})
	}
	return result
}

func (n *ClnNode) GetChannels() []*ChannelDetails {
	resp, err := n.runtime.rpc.ListPeerChannels(n.harness.Ctx, &rpc.ListpeerchannelsRequest{})
	CheckError(n.harness.T, err)

	var result []*ChannelDetails
	for _, c := range resp.Channels {
		var s ShortChannelID = NewShortChanIDFromInt(0)
		if c.ShortChannelId != nil {
			s = NewShortChanIDFromString(*c.ShortChannelId)
		}
		var localAlias *ShortChannelID
		var remoteAlias *ShortChannelID
		if c.Alias != nil {
			if c.Alias.Local != nil {
				l := NewShortChanIDFromString(*c.Alias.Local)
				localAlias = &l
			}

			if c.Alias.Remote != nil {
				r := NewShortChanIDFromString(*c.Alias.Remote)
				remoteAlias = &r
			}
		}
		result = append(result, &ChannelDetails{
			PeerId:              c.PeerId,
			ShortChannelID:      s,
			CapacityMsat:        c.TotalMsat.Msat,
			LocalReserveMsat:    c.OurReserveMsat.Msat,
			RemoteReserveMsat:   c.TheirReserveMsat.Msat,
			LocalSpendableMsat:  c.SpendableMsat.Msat,
			RemoteSpendableMsat: c.ReceivableMsat.Msat,
			LocalAlias:          localAlias,
			RemoteAlias:         remoteAlias,
		})
	}

	return result
}

func (n *ClnNode) startPayViaRoute(amountMsat uint64, paymentHash []byte, paymentSecret []byte, route *Route) (*rpc.SendpayResponse, error) {
	var sendPayRoute []*rpc.SendpayRoute
	for _, hop := range route.Hops {
		sendPayRoute = append(sendPayRoute, &rpc.SendpayRoute{
			AmountMsat: &rpc.Amount{
				Msat: hop.AmountMsat,
			},
			Id:      hop.Id,
			Delay:   uint32(hop.Delay),
			Channel: hop.Channel.String(),
		})
	}

	resp, err := n.runtime.rpc.SendPay(n.harness.Ctx, &rpc.SendpayRequest{
		Route:       sendPayRoute,
		PaymentHash: paymentHash,
		AmountMsat: &rpc.Amount{
			Msat: amountMsat,
		},
		PaymentSecret: paymentSecret,
	})

	return resp, err
}

func (n *ClnNode) PayViaRoute(
	amountMsat uint64,
	paymentHash []byte,
	paymentSecret []byte,
	route *Route,
) (*PayResult, error) {
	resp, err := n.startPayViaRoute(amountMsat, paymentHash, paymentSecret, route)
	if err != nil {
		return nil, err
	}

	t := getTimeoutSeconds(n.harness.T, n.harness.Deadline())
	w, err := n.runtime.rpc.WaitSendPay(n.harness.Ctx, &rpc.WaitsendpayRequest{
		PaymentHash: resp.PaymentHash,
		Timeout:     &t,
		Partid:      resp.Partid,
		Groupid:     resp.Groupid,
	})
	if err != nil {
		return nil, err
	}

	return &PayResult{
		PaymentHash:     w.PaymentHash,
		AmountMsat:      w.AmountMsat.Msat,
		Destination:     w.Destination,
		AmountSentMsat:  w.AmountSentMsat.Msat,
		PaymentPreimage: w.PaymentPreimage,
	}, nil
}

func (n *ClnNode) GetInvoice(paymentHash []byte) *GetInvoiceResponse {
	resp, err := n.runtime.rpc.ListInvoices(n.harness.Ctx, &rpc.ListinvoicesRequest{
		PaymentHash: paymentHash,
	})
	CheckError(n.harness.T, err)
	if resp.Invoices == nil || len(resp.Invoices) == 0 {
		return &GetInvoiceResponse{
			Exists: false,
		}
	}
	invoice := resp.Invoices[0]
	return &GetInvoiceResponse{
		Exists:             true,
		AmountMsat:         invoice.AmountMsat.Msat,
		AmountReceivedMsat: invoice.AmountReceivedMsat.Msat,
		Bolt11:             invoice.Bolt11,
		Description:        invoice.Description,
		ExpiresAt:          invoice.ExpiresAt,
		PaidAt:             invoice.PaidAt,
		PayerNote:          invoice.InvreqPayerNote,
		PaymentHash:        invoice.PaymentHash,
		PaymentPreimage:    invoice.PaymentPreimage,
		IsPaid:             invoice.Status == rpc.ListinvoicesInvoices_PAID,
		IsExpired:          invoice.Status == rpc.ListinvoicesInvoices_EXPIRED,
	}
}

func (n *ClnNode) GetPeerFeatures(peerId []byte) map[uint32]string {
	resp, err := n.runtime.rpc.ListPeers(n.harness.Ctx, &rpc.ListpeersRequest{
		Id: peerId,
	})
	CheckError(n.harness.T, err)

	r := make(map[uint32]string)
	if len(resp.Peers) == 0 {
		return r
	}
	node := resp.Peers[0]
	return n.mapFeatures(node.Features)
}

func (n *ClnNode) GetRemoteNodeFeatures(nodeId []byte) map[uint32]string {
	resp, err := n.runtime.rpc.ListNodes(n.harness.Ctx, &rpc.ListnodesRequest{
		Id: nodeId,
	})
	CheckError(n.harness.T, err)

	if len(resp.Nodes) == 0 {
		return make(map[uint32]string)
	}
	node := resp.Nodes[0]
	return n.mapFeatures(node.Features)
}

func (n *ClnNode) SendCustomMessage(req *CustomMsgRequest) {
	nodeid, err := hex.DecodeString(req.PeerId)
	CheckError(n.harness.T, err)

	var t [2]byte
	binary.BigEndian.PutUint16(t[:], uint16(req.Type))
	msg := append(t[:], req.Data...)
	_, err = n.runtime.rpc.SendCustomMsg(n.harness.Ctx, &rpc.SendcustommsgRequest{
		NodeId: nodeid,
		Msg:    msg,
	})
	CheckError(n.harness.T, err)
}

func (n *ClnNode) mapFeatures(f []byte) map[uint32]string {
	r := make(map[uint32]string)
	for i := 0; i < len(f); i++ {
		b := f[i]
		for j := 0; j < 8; j++ {
			if ((b >> j) & 1) != 0 {
				r[uint32(i)*8+uint32(j)] = ""
			}
		}
	}
	return r
}

func reverseBytes(b []byte) []byte {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}

	return b
}

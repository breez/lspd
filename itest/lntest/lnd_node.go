package lntest

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/lightningnetwork/lnd/aezeed"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"golang.org/x/exp/slices"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type LndNode struct {
	name           string
	binary         string
	args           []string
	nodeId         []byte
	harness        *TestHarness
	miner          *Miner
	dir            string
	host           string
	port           uint32
	grpcHost       string
	grpcPort       uint32
	grpcAddress    string
	tlsCert        []byte
	macaroon       []byte
	privkey        *secp256k1.PrivateKey
	walletPassword []byte
	isInitialized  bool
	logFilePath    string
	runtime        *lndNodeRuntime
	mtx            sync.Mutex
}

type lndNodeRuntime struct {
	cmd       *exec.Cmd
	conn      *grpc.ClientConn
	rpc       lnrpc.LightningClient
	routerrpc routerrpc.RouterClient
	logFile   *os.File
	cleanups  []*Cleanup
	done      chan struct{}
}

func NewLndNode(h *TestHarness, m *Miner, name string, extraArgs ...string) *LndNode {
	binary, err := GetLndBinary()
	CheckError(h.T, err)

	return NewLndNodeFromBinary(h, m, name, binary, extraArgs...)
}

func NewLndNodeFromBinary(h *TestHarness, m *Miner, name string, binary string, extraArgs ...string) *LndNode {
	lndDir := h.GetDirectory(fmt.Sprintf("lnd-%s", name))
	log.Printf("%s: Creating LND node in dir %s", name, lndDir)
	host := "127.0.0.1"
	port, err := GetPort()
	CheckError(h.T, err)

	grpcPort, err := GetPort()
	CheckError(h.T, err)

	restPort, err := GetPort()
	CheckError(h.T, err)

	grpcAddress := fmt.Sprintf("%s:%d", host, grpcPort)
	restAddress := fmt.Sprintf("%s:%d", host, restPort)
	args := append([]string{
		fmt.Sprintf("--lnddir=%s", lndDir),
		"--debuglevel=debug",
		"--nobootstrap",
		fmt.Sprintf("--rpclisten=%s", grpcAddress),
		fmt.Sprintf("--restlisten=%s", restAddress),
		fmt.Sprintf("--listen=%s:%d", host, port),
		fmt.Sprintf("--trickledelay=%d", 50),
		"--keep-failed-payment-attempts",
		"--bitcoin.active",
		"--bitcoin.node=bitcoind",
		"--bitcoin.regtest",
		fmt.Sprintf("--bitcoind.rpchost=localhost:%d", m.rpcPort),
		fmt.Sprintf("--bitcoind.rpcuser=%s", m.rpcUser),
		fmt.Sprintf("--bitcoind.rpcpass=%s", m.rpcPass),
		fmt.Sprintf("--bitcoind.zmqpubrawblock=%s", m.zmqBlockAddress),
		fmt.Sprintf("--bitcoind.zmqpubrawtx=%s", m.zmqTxAddress),
		"--gossip.channel-update-interval=10ms",
		"--db.batch-commit-interval=10ms",
	}, extraArgs...)

	logFilePath := filepath.Join(lndDir, "lnd-stdouterr.log")
	node := &LndNode{
		name:           name,
		binary:         binary,
		args:           args,
		harness:        h,
		miner:          m,
		dir:            lndDir,
		port:           port,
		host:           host,
		grpcHost:       host,
		grpcPort:       grpcPort,
		grpcAddress:    grpcAddress,
		logFilePath:    logFilePath,
		walletPassword: []byte("super-secret-password"),
	}

	h.AddStoppable(node)
	h.RegisterLogfile(filepath.Join(lndDir, "logs", "bitcoin", "regtest", "lnd.log"), fmt.Sprintf("lnd-%s", name))
	h.RegisterLogfile(logFilePath, fmt.Sprintf("lnd-stdout-%s", name))

	return node
}

func (n *LndNode) Start() {
	n.mtx.Lock()
	defer n.mtx.Unlock()

	if n.runtime != nil {
		log.Printf("%s: Start called, but was already started.", n.name)
		return
	}

	var cleanups []*Cleanup
	cmd := exec.CommandContext(n.harness.Ctx, n.binary, n.args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	logFile, err := os.OpenFile(
		n.logFilePath,
		os.O_RDWR|os.O_CREATE|os.O_APPEND,
		0666,
	)
	CheckError(n.harness.T, err)
	cleanups = append(cleanups, &Cleanup{
		Name: fmt.Sprintf("%s: logfile", n.name),
		Fn:   logFile.Close,
	})
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	log.Printf("%s: starting %s on port %d in dir %s...", n.name, n.binary, n.port, n.dir)

	err = cmd.Start()
	if err != nil {
		PerformCleanup(cleanups)
		log.Fatalf("%s: Failed to start LND: %v", n.name, err)
	}

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
		err := cmd.Wait()
		if err != nil && err.Error() != "signal: interrupt" {
			log.Printf("%s: lnd exited with error %s", n.name, err)
		} else {
			log.Printf("%s: process exited normally.", n.name)
		}
		close(done)
	}()

	tlsCert, tlsCreds, err := n.waitForTlsCert()
	if err != nil {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: %v", n.name, err)
	}
	n.tlsCert = tlsCert

	opts := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithTransportCredentials(tlsCreds),
	}

	tmpConn, err := grpc.DialContext(n.harness.Ctx, n.grpcAddress, opts...)
	if err != nil {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: failed to create grpc connection: %v", n.name, err)
	}
	defer tmpConn.Close()

	err = n.waitServerStarted(tmpConn)
	if err != nil {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: waitServerStarted: %v", n.name, err)
	}

	if n.isInitialized {
		err = n.unlockWallet(tmpConn)
		if err != nil {
			PerformCleanup(cleanups)
			n.harness.T.Fatalf("%s: unlockWallet: %v", n.name, err)
		}
	} else {
		mac, priv, _, err := n.initWallet(tmpConn)
		if err != nil {
			PerformCleanup(cleanups)
			n.harness.T.Fatalf("%s: initWallet: %v", n.name, err)
		}
		n.macaroon = mac
		n.privkey = priv
		n.isInitialized = true
	}

	err = n.waitServerActive(tmpConn)
	if err != nil {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: waitServerActive: %v", n.name, err)
	}

	macCred := NewMacaroonCredential(n.macaroon)
	opts = append(opts, grpc.WithPerRPCCredentials(macCred))

	conn, err := grpc.DialContext(n.harness.Ctx, n.grpcAddress, opts...)
	if err != nil {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: failed to create grpc connection: %v", n.name, err)
	}
	cleanups = append(cleanups, &Cleanup{
		Name: fmt.Sprintf("%s: grpc conn", n.name),
		Fn:   conn.Close,
	})

	client := lnrpc.NewLightningClient(conn)
	routerclient := routerrpc.NewRouterClient(conn)
	info, err := client.GetInfo(n.harness.Ctx, &lnrpc.GetInfoRequest{})
	if err != nil {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: failed to call getinfo: %v", n.name, err)
	}

	log.Printf("%s: Has node id %s", n.name, info.IdentityPubkey)

	var features string
	for i, f := range info.Features {
		features += strconv.FormatUint(uint64(i), 10)
		features += ":"
		features += f.Name
	}
	log.Printf("%s: Has features: %s", n.name, features)
	nodeId, err := hex.DecodeString(info.IdentityPubkey)
	if err != nil {
		PerformCleanup(cleanups)
		n.harness.T.Fatalf("%s: failed to decode node id '%s': %v", n.name, info.IdentityPubkey, err)
	}

	n.nodeId = nodeId
	n.runtime = &lndNodeRuntime{
		cmd:       cmd,
		conn:      conn,
		rpc:       client,
		routerrpc: routerclient,
		logFile:   logFile,
		cleanups:  cleanups,
		done:      done,
	}
}

func (n *LndNode) Stop() error {
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

func (n *LndNode) waitForTlsCert() ([]byte, credentials.TransportCredentials, error) {
	tlsCertPath := filepath.Join(n.dir, "tls.cert")
	var tlsCreds credentials.TransportCredentials
	var err error
	for {
		tlsCreds, err = credentials.NewClientTLSFromFile(
			tlsCertPath,
			"",
		)

		if err == nil {
			break
		}

		if time.Now().After(n.harness.Deadline()) {
			return nil, nil, fmt.Errorf("tls.cert not created before timeout")
		}

		log.Printf("%s: Waiting for tls cert to appear. %v", n.name, err)
		<-time.After(50 * time.Millisecond)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("hanging error after tls.cert appeared")
	}

	tlsCert, err := os.ReadFile(tlsCertPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read tls.cert file")
	}

	return tlsCert, tlsCreds, nil
}

func (n *LndNode) IsStarted() bool {
	return n.runtime != nil
}

func (n *LndNode) NodeId() []byte {
	return n.nodeId
}

func (n *LndNode) Host() string {
	return n.host
}

func (n *LndNode) Port() uint32 {
	return n.port
}

func (n *LndNode) PrivateKey() *secp256k1.PrivateKey {
	return n.privkey
}

func (n *LndNode) TlsCert() []byte {
	return n.tlsCert
}

func (n *LndNode) TlsCertPath() string {
	return filepath.Join(n.dir, "tls.cert")
}

func (n *LndNode) Macaroon() []byte {
	return n.macaroon
}

func (n *LndNode) MacaroonPath() string {
	return filepath.Join(n.dir, "data/chain/bitcoin/regtest/admin.macaroon")
}

func (n *LndNode) GrpcHost() string {
	return fmt.Sprintf("%s:%d", n.grpcHost, n.grpcPort)
}

func (n *LndNode) WaitForSync() {
	for {
		info, _ := n.runtime.rpc.GetInfo(n.harness.Ctx, &lnrpc.GetInfoRequest{})

		blockHeight := n.miner.GetBlockHeight()

		if info.BlockHeight >= blockHeight {
			log.Printf("%s: Synced to blockheight %d", n.name, blockHeight)
			break
		}

		log.Printf(
			"%s: Waiting to sync. Actual block height: %d, node block height: %d",
			n.name,
			blockHeight,
			info.BlockHeight,
		)

		if time.Now().After(n.harness.Deadline()) {
			n.harness.T.Fatalf("%s: timed out waiting for channel normal", n.name)
		}

		<-time.After(waitSleepInterval)
	}
}

func (n *LndNode) GetNewAddress() string {
	addrResponse, err := n.runtime.rpc.NewAddress(
		n.harness.Ctx,
		&lnrpc.NewAddressRequest{
			Type: lnrpc.AddressType_UNUSED_TAPROOT_PUBKEY,
		},
	)
	CheckError(n.harness.T, err)
	return addrResponse.Address
}

func (n *LndNode) SendToAddress(addr string, amountSat uint64) {
	_, err := n.runtime.rpc.SendCoins(n.harness.Ctx, &lnrpc.SendCoinsRequest{
		Addr:   addr,
		Amount: int64(amountSat),
	})
	CheckError(n.harness.T, err)
}

func (n *LndNode) Fund(amountSat uint64) {
	addr := n.GetNewAddress()
	n.miner.SendToAddressAndMine(addr, amountSat, 1)
	n.WaitForSync()
}

func (n *LndNode) ConnectPeer(peer LightningNode) {
	_, err := n.runtime.rpc.ConnectPeer(n.harness.Ctx, &lnrpc.ConnectPeerRequest{
		Addr: &lnrpc.LightningAddress{
			Pubkey: hex.EncodeToString(peer.NodeId()),
			Host:   fmt.Sprintf("%s:%d", peer.Host(), peer.Port()),
		},
	})

	if err != nil && strings.Contains(err.Error(), "already connected to peer") {
		return
	}

	CheckError(n.harness.T, err)
}

func (n *LndNode) OpenChannel(peer LightningNode, options *OpenChannelOptions) *ChannelInfo {
	n.ConnectPeer(peer)

	// open a channel
	fundResult, err := n.runtime.rpc.OpenChannelSync(n.harness.Ctx, &lnrpc.OpenChannelRequest{
		NodePubkey:         peer.NodeId(),
		LocalFundingAmount: int64(options.AmountSat),
		Private:            !options.IsPublic,
	})
	CheckError(n.harness.T, err)

	return &ChannelInfo{
		From:            n,
		To:              peer,
		FundingTxId:     fundResult.GetFundingTxidBytes(),
		FundingTxOutnum: fundResult.OutputIndex,
	}
}

func (n *LndNode) WaitForChannelReady(channel *ChannelInfo) ShortChannelID {
	peerId := channel.GetPeer(n).NodeId()
	peerIdStr := hex.EncodeToString(peerId)
	ch, err := chainhash.NewHash(channel.FundingTxId)
	CheckError(n.harness.T, err)
	txidStr := ch.String()

	for {
		lc, err := n.runtime.rpc.ListChannels(n.harness.Ctx, &lnrpc.ListChannelsRequest{
			Peer: peerId,
		})
		CheckError(n.harness.T, err)

		index := slices.IndexFunc(lc.Channels, func(c *lnrpc.Channel) bool {
			s := strings.Split(c.ChannelPoint, ":")
			txid := s[0]
			out, err := strconv.ParseUint(s[1], 10, 32)
			CheckError(n.harness.T, err)

			return c.RemotePubkey == peerIdStr &&
				txid == txidStr &&
				out == uint64(channel.FundingTxOutnum)
		})

		if index >= 0 {
			c := lc.Channels[index]
			if c.Active {
				if c.Private && len(c.AliasScids) > 0 {
					return NewShortChanIDFromInt(c.AliasScids[0])
				} else {
					return NewShortChanIDFromInt(c.ChanId)
				}
			}
		} else {

			pending, err := n.runtime.rpc.PendingChannels(n.harness.Ctx, &lnrpc.PendingChannelsRequest{})
			CheckError(n.harness.T, err)

			pendingIndex := slices.IndexFunc(pending.PendingOpenChannels, func(c *lnrpc.PendingChannelsResponse_PendingOpenChannel) bool {
				s := strings.Split(c.Channel.ChannelPoint, ":")
				txid := s[0]
				out, err := strconv.ParseUint(s[1], 10, 32)
				CheckError(n.harness.T, err)
				return c.Channel.RemoteNodePub == peerIdStr &&
					txid == txidStr &&
					out == uint64(channel.FundingTxOutnum)
			})

			if pendingIndex >= 0 {
				log.Printf("%s: Channel is pending. Mining some blocks.", n.name)
				n.miner.MineBlocks(6)
				n.WaitForSync()
				continue
			}
		}

		if time.Now().After(n.harness.Deadline()) {
			n.harness.T.Fatalf("%s: timed out waiting for channel normal", n.name)
		}

		<-time.After(waitSleepInterval)
	}
}
func (n *LndNode) CreateBolt11Invoice(options *CreateInvoiceOptions) *CreateInvoiceResult {
	req := &lnrpc.Invoice{
		ValueMsat: int64(options.AmountMsat),
		Private:   options.IncludeHopHints,
	}

	if options.Description != nil {
		req.Memo = *options.Description
	}

	if options.Preimage != nil {
		req.RPreimage = *options.Preimage
	}

	if options.Cltv != nil {
		req.CltvExpiry = uint64(*options.Cltv)
	}

	resp, err := n.runtime.rpc.AddInvoice(n.harness.Ctx, req)
	CheckError(n.harness.T, err)

	return &CreateInvoiceResult{
		Bolt11:        resp.PaymentRequest,
		PaymentHash:   resp.RHash,
		PaymentSecret: resp.PaymentAddr,
	}
}

func (n *LndNode) SignMessage(message []byte) []byte {
	resp, err := n.runtime.rpc.SignMessage(n.harness.Ctx, &lnrpc.SignMessageRequest{
		Msg: message,
	})
	CheckError(n.harness.T, err)

	sig, err := hex.DecodeString(resp.Signature)
	CheckError(n.harness.T, err)

	return sig
}

func (n *LndNode) Pay(bolt11 string) *PayResult {
	ctx, cancel := context.WithCancel(n.harness.Ctx)
	defer cancel()
	resp, err := n.runtime.routerrpc.SendPaymentV2(ctx, &routerrpc.SendPaymentRequest{
		PaymentRequest: bolt11,
		TimeoutSeconds: 60,
		FeeLimitSat:    10000,
		CltvLimit:      2016,
	})
	CheckError(n.harness.T, err)

	for {
		payment, err := resp.Recv()
		CheckError(n.harness.T, err)

		switch payment.Status {
		case lnrpc.Payment_SUCCEEDED:
			paymentHash, err := hex.DecodeString(payment.PaymentHash)
			CheckError(n.harness.T, err)
			preimage, err := hex.DecodeString(payment.PaymentPreimage)
			CheckError(n.harness.T, err)
			return &PayResult{
				PaymentHash:     paymentHash,
				AmountMsat:      uint64(payment.ValueMsat),
				AmountSentMsat:  uint64(payment.ValueMsat + payment.FeeMsat),
				PaymentPreimage: preimage,
			}
		case lnrpc.Payment_IN_FLIGHT:
			if len(payment.Htlcs) > 0 {
				attempt := payment.Htlcs[len(payment.Htlcs)-1]
				if attempt.Failure != nil {
					log.Printf("%s: htlc attempt %d failed with code %v from index %d", n.name, attempt.AttemptId, attempt.Failure.Code, attempt.Failure.FailureSourceIndex)
				}
			}
		case lnrpc.Payment_FAILED:
			n.harness.T.Fatalf("%s: payment failed with reason: %v", n.name, payment.FailureReason)
		case lnrpc.Payment_UNKNOWN:
			log.Printf("%s: got UNKNOWN payment update: %+v", n.name, payment)
		}
	}
}

func (n *LndNode) GetRoute(destination []byte, amountMsat uint64) *Route {
	routes, err := n.runtime.rpc.QueryRoutes(n.harness.Ctx, &lnrpc.QueryRoutesRequest{
		PubKey:  hex.EncodeToString(destination),
		AmtMsat: int64(amountMsat),
	})
	CheckError(n.harness.T, err)

	if routes.Routes == nil || len(routes.Routes) == 0 {
		CheckError(n.harness.T, fmt.Errorf("no route found"))
	}

	route := routes.Routes[0]
	result := &Route{}
	for _, hop := range route.Hops {
		id, err := hex.DecodeString(hop.PubKey)
		CheckError(n.harness.T, err)

		result.Hops = append(result.Hops, &Hop{
			Id:         id,
			Channel:    NewShortChanIDFromInt(hop.ChanId),
			AmountMsat: uint64(hop.AmtToForwardMsat),
			Delay:      uint16(hop.Expiry),
		})
	}

	return result
}

func (n *LndNode) PayViaRoute(amountMsat uint64, paymentHash []byte, paymentSecret []byte, route *Route) (*PayResult, error) {
	r := &lnrpc.Route{}

	for _, hop := range route.Hops {
		r.Hops = append(r.Hops, &lnrpc.Hop{
			ChanId:           hop.Channel.ToUint64(),
			Expiry:           uint32(hop.Delay),
			PubKey:           hex.EncodeToString(hop.Id),
			AmtToForwardMsat: int64(hop.AmountMsat),
		})
	}

	lh := r.Hops[len(r.Hops)-1]
	lh.MppRecord = &lnrpc.MPPRecord{
		TotalAmtMsat: int64(amountMsat),
		PaymentAddr:  paymentSecret,
	}

	resp, err := n.runtime.rpc.SendToRouteSync(n.harness.Ctx, &lnrpc.SendToRouteRequest{
		PaymentHash: paymentHash,
		Route:       r,
	})
	if err != nil {
		return nil, err
	}

	return &PayResult{
		PaymentHash:     resp.PaymentHash,
		AmountMsat:      uint64(resp.PaymentRoute.TotalAmtMsat) - uint64(resp.PaymentRoute.TotalFeesMsat),
		AmountSentMsat:  uint64(resp.PaymentRoute.TotalAmtMsat),
		PaymentPreimage: resp.PaymentPreimage,
	}, nil
}

func (n *LndNode) GetInvoice(paymentHash []byte) *GetInvoiceResponse {
	resp, err := n.runtime.rpc.LookupInvoice(n.harness.Ctx, &lnrpc.PaymentHash{
		RHash: paymentHash,
	})
	CheckError(n.harness.T, err)

	var paidAt *uint64
	if resp.SettleDate != 0 {
		p := uint64(resp.SettleDate)
		paidAt = &p
	}

	return &GetInvoiceResponse{
		Exists:             true,
		AmountMsat:         uint64(resp.ValueMsat),
		AmountReceivedMsat: uint64(resp.AmtPaidMsat),
		Bolt11:             &resp.PaymentRequest,
		Description:        &resp.Memo,
		ExpiresAt:          uint64(resp.Expiry),
		PaidAt:             paidAt,
		PaymentHash:        resp.RHash,
		PaymentPreimage:    resp.RPreimage,
		IsPaid:             resp.State == lnrpc.Invoice_SETTLED,
		IsExpired:          resp.State == lnrpc.Invoice_CANCELED,
	}
}

func (n *LndNode) GetPeerFeatures(peerId []byte) map[uint32]string {
	pubkey := hex.EncodeToString(peerId)
	resp, err := n.runtime.rpc.ListPeers(n.harness.Ctx, &lnrpc.ListPeersRequest{})
	CheckError(n.harness.T, err)

	for _, p := range resp.Peers {
		if p.PubKey == pubkey {
			return n.mapFeatures(p.Features)
		}
	}

	return make(map[uint32]string)
}

func (n *LndNode) mapFeatures(features map[uint32]*lnrpc.Feature) map[uint32]string {
	r := make(map[uint32]string)
	for i, f := range features {
		r[i] = f.Name
	}

	return r
}

func (n *LndNode) GetRemoteNodeFeatures(nodeId []byte) map[uint32]string {
	resp, err := n.runtime.rpc.GetNodeInfo(n.harness.Ctx, &lnrpc.NodeInfoRequest{
		PubKey: hex.EncodeToString(nodeId),
	})
	CheckError(n.harness.T, err)

	r := make(map[uint32]string)
	for i, f := range resp.Node.Features {
		r[i] = f.Name
	}

	return r
}

func (n *LndNode) GetChannels() []*ChannelDetails {
	channels, err := n.runtime.rpc.ListChannels(n.harness.Ctx, &lnrpc.ListChannelsRequest{})
	CheckError(n.harness.T, err)

	var result []*ChannelDetails
	for _, c := range channels.Channels {
		p, _ := hex.DecodeString(c.RemotePubkey)
		var localAlias *ShortChannelID
		var remoteAlias *ShortChannelID
		if len(c.AliasScids) > 0 {
			l := NewShortChanIDFromInt(c.AliasScids[0])
			localAlias = &l
		}
		if len(c.AliasScids) > 1 {
			r := NewShortChanIDFromInt(c.AliasScids[1])
			remoteAlias = &r
		}
		result = append(result, &ChannelDetails{
			PeerId:              p,
			ShortChannelID:      NewShortChanIDFromInt(c.ChanId),
			CapacityMsat:        uint64(c.Capacity) * 1000,
			LocalReserveMsat:    c.LocalConstraints.ChanReserveSat * 1000,
			RemoteReserveMsat:   c.RemoteConstraints.ChanReserveSat * 1000,
			LocalSpendableMsat:  uint64(c.LocalBalance) - c.LocalConstraints.ChanReserveSat*1000,
			RemoteSpendableMsat: uint64(c.RemoteBalance) - c.RemoteConstraints.ChanReserveSat*1000,
			LocalAlias:          localAlias,
			RemoteAlias:         remoteAlias,
		})
	}

	return result
}

func (n *LndNode) Conn() grpc.ClientConnInterface {
	return n.runtime.conn
}

func (n *LndNode) LightningClient() lnrpc.LightningClient {
	return n.runtime.rpc
}

func (n *LndNode) SendCustomMessage(msg *CustomMsgRequest) {
	peer, err := hex.DecodeString(msg.PeerId)
	CheckError(n.harness.T, err)

	_, err = n.runtime.rpc.SendCustomMessage(n.harness.Ctx, &lnrpc.SendCustomMessageRequest{
		Peer: peer,
		Type: msg.Type,
		Data: msg.Data,
	})
	CheckError(n.harness.T, err)
}

func (n *LndNode) waitServerActive(conn grpc.ClientConnInterface) error {
	log.Printf("%s: Waiting for LND rpc to be fully active.", n.name)
	return n.waitServerState(conn, func(s lnrpc.WalletState) bool {
		return s == lnrpc.WalletState_SERVER_ACTIVE
	})
}

func (n *LndNode) waitServerStarted(conn grpc.ClientConnInterface) error {
	log.Printf("%s: Waiting for LND rpc to start.", n.name)
	return n.waitServerState(conn, func(s lnrpc.WalletState) bool {
		return s != lnrpc.WalletState_WAITING_TO_START
	})
}

func (n *LndNode) waitServerState(conn grpc.ClientConnInterface, pred func(s lnrpc.WalletState) bool) error {
	state := lnrpc.NewStateClient(conn)
	client, err := state.SubscribeState(n.harness.Ctx, &lnrpc.SubscribeStateRequest{})
	if err != nil {
		return fmt.Errorf("subscribe state failed: %w", err)
	}

	errChan := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		for {
			resp, err := client.Recv()
			if err != nil {
				errChan <- err
				return
			}

			log.Printf("%s: Wallet state: %v", n.name, resp.State)
			if pred(resp.State) {
				close(done)
				return
			}
		}
	}()

	var lastErr error
	for {
		select {
		case err := <-errChan:
			lastErr = err

		case <-done:
			return nil

		case <-time.After(time.Until(n.harness.Deadline())):
			return fmt.Errorf("timeout waiting for wallet state. last error: %w", lastErr)
		}
	}
}

func (n *LndNode) unlockWallet(conn grpc.ClientConnInterface) error {
	log.Printf("%s: Unlocking LND wallet.", n.name)
	c := lnrpc.NewWalletUnlockerClient(conn)
	_, err := c.UnlockWallet(n.harness.Ctx, &lnrpc.UnlockWalletRequest{
		WalletPassword: n.walletPassword,
	})

	if err != nil {
		return fmt.Errorf("failed to unlock wallet: %w", err)
	}

	return nil
}

func (n *LndNode) initWallet(conn grpc.ClientConnInterface) ([]byte, *secp256k1.PrivateKey, *btcec.PublicKey, error) {
	log.Printf("%s: Initializing LND wallet.", n.name)
	c := lnrpc.NewWalletUnlockerClient(conn)

	seed, err := c.GenSeed(n.harness.Ctx, &lnrpc.GenSeedRequest{})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to gen seed: %w", err)
	}

	resp, err := c.InitWallet(n.harness.Ctx, &lnrpc.InitWalletRequest{
		WalletPassword:     n.walletPassword,
		CipherSeedMnemonic: seed.CipherSeedMnemonic,
		StatelessInit:      false,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to init wallet: %w", err)
	}

	var mnemonic aezeed.Mnemonic
	copy(mnemonic[:], seed.CipherSeedMnemonic)

	cipherSeed, err := mnemonic.ToCipherSeed(nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to obtain cipherseed: %w", err)
	}

	rootKey, err := hdkeychain.NewMaster(
		cipherSeed.Entropy[:],
		&chaincfg.RegressionNetParams,
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create master key: %w", err)
	}

	// node identity derivation path = "m/1017'/1'/6'/0/0"
	k, err := rootKey.DeriveNonStandard(1017 + 2147483648)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to derive child 1: %w", err)
	}
	k, err = k.DeriveNonStandard(1 + 2147483648)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to derive child 2: %w", err)
	}
	k, err = hdkeychain.NewKeyFromString(k.String())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to convert child 2 to/from string: %w", err)
	}
	k, err = k.DeriveNonStandard(6 + 2147483648)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to derive child 3: %w", err)
	}
	k, err = hdkeychain.NewKeyFromString(k.String())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to convert child 3 to/from string: %w", err)
	}
	k, err = k.DeriveNonStandard(0)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to derive child 4: %w", err)
	}
	nodeKey, err := k.DeriveNonStandard(0)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to derive child 5: %w", err)
	}
	privKey, err := nodeKey.ECPrivKey()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get ECPrivKey: %w", err)
	}
	pubKey, err := nodeKey.ECPubKey()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get ECPubKey: %w", err)
	}
	return resp.AdminMacaroon, privKey, pubKey, nil
}

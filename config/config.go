package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/breez/lspd/chain"
)

type Config struct {
	Nodes             []*NodeConfig
	FeeStrategy       chain.FeeStrategy
	MempoolApiBaseUrl *string
	DatabaseUrl       string
	AutoMigrateDb     bool
	ListenAddress     string
	CertmagicDomain   string
	OpenChannelEmail  *Email
}

func LoadConfig() (*Config, error) {
	n := os.Getenv("NODES")
	var nodes []*NodeConfig
	err := json.Unmarshal([]byte(n), &nodes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal NODES env: %v", err)
	}

	if len(nodes) == 0 {
		return nil, errors.New("need at least one node configured in NODES")
	}

	var feeStrategy chain.FeeStrategy
	envFeeStrategy := os.Getenv("MEMPOOL_PRIORITY")
	switch strings.ToLower(envFeeStrategy) {
	case "minimum":
		feeStrategy = chain.FeeStrategyMinimum
	case "economy":
		feeStrategy = chain.FeeStrategyEconomy
	case "hour":
		feeStrategy = chain.FeeStrategyHour
	case "halfhour":
		feeStrategy = chain.FeeStrategyHalfHour
	case "fastest":
		feeStrategy = chain.FeeStrategyFastest
	default:
		feeStrategy = chain.FeeStrategyEconomy
	}

	var mempoolUrl *string
	mempoolUrlEnv := os.Getenv("MEMPOOL_API_BASE_URL")
	if mempoolUrlEnv != "" {
		mempoolUrl = &mempoolUrlEnv
	}

	databaseUrl := os.Getenv("DATABASE_URL")
	if databaseUrl == "" {
		return nil, fmt.Errorf("missing environment variable DATABASE_URL")
	}

	automigrate, _ := strconv.ParseBool(os.Getenv("AUTO_MIGRATE_DATABASE"))
	address := os.Getenv("LISTEN_ADDRESS")
	certMagicDomain := os.Getenv("CERTMAGIC_DOMAIN")
	openChannelEmail, err := loadOpenChannelEmailSettings()
	if err != nil {
		return nil, err
	}
	return &Config{
		Nodes:             nodes,
		FeeStrategy:       feeStrategy,
		MempoolApiBaseUrl: mempoolUrl,
		DatabaseUrl:       databaseUrl,
		AutoMigrateDb:     automigrate,
		ListenAddress:     address,
		CertmagicDomain:   certMagicDomain,
		OpenChannelEmail:  openChannelEmail,
	}, nil
}

package config

import (
	"fmt"
	"os"

	sdkconfig "github.com/shinzonetwork/shinzo-app-sdk/pkg/config"
	"gopkg.in/yaml.v3"
)

// Config is the full scheduler configuration. DefraDB and logger settings
// come from the SDK's config struct so the same YAML keys work everywhere.
type Config struct {
	sdkconfig.Config `yaml:",inline"`
	Scheduler        SchedulerConfig `yaml:"scheduler"`
}

type SchedulerConfig struct {
	Chain      string           `yaml:"chain"`
	Network    string           `yaml:"network"`
	Server     ServerConfig     `yaml:"server"`
	Probe      ProbeConfig      `yaml:"probe"`
	Auth       AuthConfig       `yaml:"auth"`
	ShinzoHub  ShinzoHubConfig  `yaml:"shinzohub"`
	Pricing    PricingConfig    `yaml:"pricing"`
	Bootstrap  BootstrapConfig  `yaml:"bootstrap"`
	Diversity  DiversityConfig  `yaml:"diversity"`
	Accounting AccountingConfig `yaml:"accounting"`
	Settlement SettlementConfig `yaml:"settlement"`
}

type ServerConfig struct {
	Port                int `yaml:"port"`
	ReadTimeoutSeconds  int `yaml:"read_timeout_seconds"`
	WriteTimeoutSeconds int `yaml:"write_timeout_seconds"`
}

type ProbeConfig struct {
	IntervalSeconds          int `yaml:"interval_seconds"`
	TimeoutSeconds           int `yaml:"timeout_seconds"`
	TipLagThreshold          int `yaml:"tip_lag_threshold"`
	TipExclusionThreshold    int `yaml:"tip_exclusion_threshold"`
	StalenessWindowSeconds   int `yaml:"staleness_window_seconds"`
	InactiveAfterMinutes     int `yaml:"inactive_after_minutes"`
	ProbeHistoryLimit        int `yaml:"probe_history_limit"`
	MaxConcurrentProbes      int `yaml:"max_concurrent_probes"`
	HeartbeatIntervalSeconds int `yaml:"heartbeat_interval_seconds"`
}

type AuthConfig struct {
	HMACSecret string `yaml:"hmac_secret"`
}

type ShinzoHubConfig struct {
	Enabled   bool   `yaml:"enabled"`
	RPCURL    string `yaml:"rpc_url"`
	EpochSize int    `yaml:"epoch_size"`
}

// PricingConfig defines the minimum pricing the scheduler will accept from indexers.
type PricingConfig struct {
	FloorTipPer1kBlocks   float64 `yaml:"floor_tip_per_1k_blocks"`
	FloorSnapshotPerRange float64 `yaml:"floor_snapshot_per_range"`
}

// DiversityConfig controls match-history-based diversity weighting in discovery.
type DiversityConfig struct {
	Enabled            bool `yaml:"enabled"`
	RecencyWindowHours int  `yaml:"recency_window_hours"`
}

// AccountingConfig controls the delivery-claim / attestation comparison subsystem.
type AccountingConfig struct {
	Enabled                   bool `yaml:"enabled"`
	ComparisonIntervalSeconds int  `yaml:"comparison_interval_seconds"`
	AttestationWindowSeconds  int  `yaml:"attestation_window_seconds"`
	UnderReportGraceSeconds   int  `yaml:"under_report_grace_seconds"`
}

// SettlementConfig controls escrow drainage, batch settlement, and verdict signing.
type SettlementConfig struct {
	Enabled                          bool    `yaml:"enabled"`
	DrainIntervalSeconds             int     `yaml:"drain_interval_seconds"`
	LowCreditMultiplier              float64 `yaml:"low_credit_multiplier"`
	GracePeriodSeconds               int     `yaml:"grace_period_seconds"`
	SettlementWindowBlocks           int     `yaml:"settlement_window_blocks"`
	VerdictThresholdM                int     `yaml:"verdict_threshold_m"`
	VerdictThresholdN                int     `yaml:"verdict_threshold_n"`
	SchedulerKeyPath                 string  `yaml:"scheduler_key_path"`
	ContradictionThreshold           int     `yaml:"contradiction_threshold"`
	ContradictionCheckIntervalSeconds int    `yaml:"contradiction_check_interval_seconds"`
}

// BootstrapConfig lists operator-owned indexers to seed on startup.
type BootstrapConfig struct {
	Indexers []BootstrapIndexer `yaml:"indexers"`
}

type BootstrapIndexer struct {
	PeerID    string `yaml:"peer_id"`
	HTTPUrl   string `yaml:"http_url"`
	Multiaddr string `yaml:"multiaddr"`
}

// Load reads the config file at path and applies env-variable overrides.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := defaults()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

func defaults() *Config {
	return &Config{
		Scheduler: SchedulerConfig{
			Server: ServerConfig{
				Port:                8090,
				ReadTimeoutSeconds:  30,
				WriteTimeoutSeconds: 30,
			},
			Probe: ProbeConfig{
				IntervalSeconds:          60,
				TimeoutSeconds:           5,
				TipLagThreshold:          10,
				TipExclusionThreshold:    50,
				StalenessWindowSeconds:   120,
				InactiveAfterMinutes:     10,
				ProbeHistoryLimit:        200,
				MaxConcurrentProbes:      20,
				HeartbeatIntervalSeconds: 30,
			},
			Diversity: DiversityConfig{
				Enabled:            true,
				RecencyWindowHours: 24,
			},
			Accounting: AccountingConfig{
				ComparisonIntervalSeconds: 30,
				AttestationWindowSeconds:  300,
				UnderReportGraceSeconds:   600,
			},
			Settlement: SettlementConfig{
				DrainIntervalSeconds:   60,
				LowCreditMultiplier:    2.0,
				GracePeriodSeconds:     3600,
				SettlementWindowBlocks: 100,
				VerdictThresholdM:                 1,
				VerdictThresholdN:                 1,
				ContradictionThreshold:            3,
				ContradictionCheckIntervalSeconds: 300,
			},
			ShinzoHub: ShinzoHubConfig{
				RPCURL: "rpc.devnet.shinzo.network:26657",
			},
		},
	}
}

// Validate checks that mandatory config fields are present and values are in range.
func (c *Config) Validate() error {
	s := c.Scheduler
	if s.Chain == "" {
		return fmt.Errorf("scheduler.chain must not be empty")
	}
	if s.Network == "" {
		return fmt.Errorf("scheduler.network must not be empty")
	}
	if s.Server.Port <= 0 || s.Server.Port > 65535 {
		return fmt.Errorf("scheduler.server.port must be in range 1-65535 (got %d)", s.Server.Port)
	}
	if s.Auth.HMACSecret == "" {
		return fmt.Errorf("scheduler.auth.hmac_secret must not be empty; set SCHEDULER_HMAC_SECRET or config key")
	}
	if s.Probe.IntervalSeconds <= 0 {
		return fmt.Errorf("scheduler.probe.interval_seconds must be > 0")
	}
	if s.Probe.TimeoutSeconds <= 0 {
		return fmt.Errorf("scheduler.probe.timeout_seconds must be > 0")
	}
	if s.Probe.MaxConcurrentProbes <= 0 {
		return fmt.Errorf("scheduler.probe.max_concurrent_probes must be > 0")
	}
	if s.Probe.HeartbeatIntervalSeconds <= 0 {
		return fmt.Errorf("scheduler.probe.heartbeat_interval_seconds must be > 0")
	}
	if s.ShinzoHub.Enabled && s.ShinzoHub.RPCURL == "" {
		return fmt.Errorf("scheduler.shinzohub.rpc_url must not be empty when shinzohub is enabled")
	}
	if s.Accounting.Enabled {
		if s.Accounting.ComparisonIntervalSeconds <= 0 {
			return fmt.Errorf("scheduler.accounting.comparison_interval_seconds must be > 0")
		}
		if s.Accounting.AttestationWindowSeconds <= 0 {
			return fmt.Errorf("scheduler.accounting.attestation_window_seconds must be > 0")
		}
	}
	if s.Settlement.Enabled {
		if s.Settlement.DrainIntervalSeconds <= 0 {
			return fmt.Errorf("scheduler.settlement.drain_interval_seconds must be > 0")
		}
		if s.Settlement.GracePeriodSeconds <= 0 {
			return fmt.Errorf("scheduler.settlement.grace_period_seconds must be > 0")
		}
		if s.Settlement.VerdictThresholdM <= 0 {
			return fmt.Errorf("scheduler.settlement.verdict_threshold_m must be > 0")
		}
		if s.Settlement.VerdictThresholdN <= 0 {
			return fmt.Errorf("scheduler.settlement.verdict_threshold_n must be > 0")
		}
		if s.Settlement.VerdictThresholdM > s.Settlement.VerdictThresholdN {
			return fmt.Errorf("scheduler.settlement.verdict_threshold_m (%d) must be <= verdict_threshold_n (%d)", s.Settlement.VerdictThresholdM, s.Settlement.VerdictThresholdN)
		}
		if s.Settlement.ContradictionThreshold <= 0 {
			return fmt.Errorf("scheduler.settlement.contradiction_threshold must be > 0")
		}
		if s.Settlement.ContradictionCheckIntervalSeconds <= 0 {
			return fmt.Errorf("scheduler.settlement.contradiction_check_interval_seconds must be > 0")
		}
	}
	return nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("DEFRA_KEYRING_SECRET"); v != "" {
		cfg.DefraDB.KeyringSecret = v
	}
	if v := os.Getenv("SCHEDULER_HMAC_SECRET"); v != "" {
		cfg.Scheduler.Auth.HMACSecret = v
	}
	if v := os.Getenv("SCHEDULER_CHAIN"); v != "" {
		cfg.Scheduler.Chain = v
	}
	if v := os.Getenv("SCHEDULER_NETWORK"); v != "" {
		cfg.Scheduler.Network = v
	}
}

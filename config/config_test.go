package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


func validConfig() *Config {
	cfg := defaults()
	cfg.Scheduler.Chain = "ethereum"
	cfg.Scheduler.Network = "mainnet"
	return cfg
}

func TestValidate_ValidConfig(t *testing.T) {
	require.NoError(t, validConfig().Validate())
}

func TestValidate_PortOutOfRange(t *testing.T) {
	cases := []int{0, -1, 65536, 99999}
	for _, port := range cases {
		cfg := validConfig()
		cfg.Scheduler.Server.Port = port
		assert.Error(t, cfg.Validate(), "expected error for port %d", port)
	}
}

func TestValidate_ProbeIntervalZero(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Probe.IntervalSeconds = 0
	assert.Error(t, cfg.Validate())
}

func TestValidate_ProbeTimeoutZero(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Probe.TimeoutSeconds = 0
	assert.Error(t, cfg.Validate())
}

func TestValidate_MaxConcurrentProbesZero(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Probe.MaxConcurrentProbes = 0
	assert.Error(t, cfg.Validate())
}

func TestValidate_ShinzoHubEnabledEmptyURL(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.ShinzoHub.Enabled = true
	cfg.Scheduler.ShinzoHub.RPCURL = ""
	assert.Error(t, cfg.Validate())
}

func TestValidate_ShinzoHubEnabledWithURL(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.ShinzoHub.Enabled = true
	cfg.Scheduler.ShinzoHub.RPCURL = "rpc.example.com:26657"
	assert.NoError(t, cfg.Validate())
}

func TestValidate_EmptyChain(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Chain = ""
	assert.Error(t, cfg.Validate())
}

func TestValidate_EmptyNetwork(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Network = ""
	assert.Error(t, cfg.Validate())
}

func TestValidate_HeartbeatIntervalZero(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Probe.HeartbeatIntervalSeconds = 0
	assert.Error(t, cfg.Validate())
}

func TestDefaults_NewConfigFields(t *testing.T) {
	cfg := defaults()
	assert.Equal(t, 50, cfg.Scheduler.Probe.TipExclusionThreshold)
	assert.Equal(t, 120, cfg.Scheduler.Probe.StalenessWindowSeconds)
	assert.True(t, cfg.Scheduler.Diversity.Enabled)
	assert.Equal(t, 24, cfg.Scheduler.Diversity.RecencyWindowHours)
	assert.Equal(t, 30, cfg.Scheduler.Accounting.ComparisonIntervalSeconds)
	assert.Equal(t, 300, cfg.Scheduler.Accounting.AttestationWindowSeconds)
	assert.Equal(t, 600, cfg.Scheduler.Accounting.UnderReportGraceSeconds)
	assert.Equal(t, 60, cfg.Scheduler.Settlement.DrainIntervalSeconds)
	assert.Equal(t, 2.0, cfg.Scheduler.Settlement.LowCreditMultiplier)
	assert.Equal(t, 3600, cfg.Scheduler.Settlement.GracePeriodSeconds)
	assert.Equal(t, 100, cfg.Scheduler.Settlement.SettlementWindowBlocks)
	assert.Equal(t, 1, cfg.Scheduler.Settlement.VerdictThresholdM)
	assert.Equal(t, 1, cfg.Scheduler.Settlement.VerdictThresholdN)
	assert.Equal(t, 3, cfg.Scheduler.Settlement.ContradictionThreshold)
	assert.Equal(t, 300, cfg.Scheduler.Settlement.ContradictionCheckIntervalSeconds)
}

func TestValidate_ContradictionThresholdZero(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Settlement.Enabled = true
	cfg.Scheduler.Settlement.ContradictionThreshold = 0
	assert.Error(t, cfg.Validate())
}

func TestValidate_ContradictionCheckIntervalZero(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Settlement.Enabled = true
	cfg.Scheduler.Settlement.ContradictionCheckIntervalSeconds = 0
	assert.Error(t, cfg.Validate())
}

func TestValidate_SettlementDisabledSkipsContradictionChecks(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Settlement.Enabled = false
	cfg.Scheduler.Settlement.ContradictionThreshold = 0
	cfg.Scheduler.Settlement.ContradictionCheckIntervalSeconds = 0
	assert.NoError(t, cfg.Validate())
}

func TestValidate_AccountingComparisonIntervalZero(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Accounting.Enabled = true
	cfg.Scheduler.Accounting.ComparisonIntervalSeconds = 0
	assert.Error(t, cfg.Validate())
}

func TestValidate_AccountingAttestationWindowZero(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Accounting.Enabled = true
	cfg.Scheduler.Accounting.AttestationWindowSeconds = 0
	assert.Error(t, cfg.Validate())
}

func TestValidate_AccountingDisabledSkipsIntervalChecks(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Accounting.Enabled = false
	cfg.Scheduler.Accounting.ComparisonIntervalSeconds = 0
	assert.NoError(t, cfg.Validate())
}

func TestValidate_SettlementDrainIntervalZero(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Settlement.Enabled = true
	cfg.Scheduler.Settlement.DrainIntervalSeconds = 0
	assert.Error(t, cfg.Validate())
}

func TestValidate_SettlementGracePeriodZero(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Settlement.Enabled = true
	cfg.Scheduler.Settlement.GracePeriodSeconds = 0
	assert.Error(t, cfg.Validate())
}

func TestValidate_VerdictThresholdMZero(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Settlement.Enabled = true
	cfg.Scheduler.Settlement.VerdictThresholdM = 0
	assert.Error(t, cfg.Validate())
}

func TestValidate_VerdictThresholdMGreaterThanN(t *testing.T) {
	cfg := validConfig()
	cfg.Scheduler.Settlement.Enabled = true
	cfg.Scheduler.Settlement.VerdictThresholdM = 5
	cfg.Scheduler.Settlement.VerdictThresholdN = 3
	assert.Error(t, cfg.Validate())
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	assert.Error(t, err)
}

func TestLoad_InvalidYAML(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString("not: valid: yaml: [[[")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_, err = Load(f.Name())
	assert.Error(t, err)
}

func TestLoad_Success(t *testing.T) {
	yaml := `
scheduler:
  chain: ethereum
  network: mainnet
`
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(yaml)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	cfg, err := Load(f.Name())
	require.NoError(t, err)
	assert.Equal(t, "ethereum", cfg.Scheduler.Chain)
	assert.Equal(t, "mainnet", cfg.Scheduler.Network)
}

func TestApplyEnvOverrides(t *testing.T) {
	t.Setenv("DEFRA_KEYRING_SECRET", "defra-secret")
	t.Setenv("SCHEDULER_CHAIN", "polygon")
	t.Setenv("SCHEDULER_NETWORK", "testnet")

	cfg := defaults()
	applyEnvOverrides(cfg)

	assert.Equal(t, "defra-secret", cfg.DefraDB.KeyringSecret)
	assert.Equal(t, "polygon", cfg.Scheduler.Chain)
	assert.Equal(t, "testnet", cfg.Scheduler.Network)
}

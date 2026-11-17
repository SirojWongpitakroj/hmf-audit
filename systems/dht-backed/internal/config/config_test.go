package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhtbacked/internal/config"
)

// validEnv writes the file-backed configuration into a temp dir and returns a
// complete, valid set of the ten Q46 variables.
func validEnv(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	keyDir := filepath.Join(dir, "producers")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	return map[string]string{
		config.EnvAEADKeyHex:       strings.Repeat("ab", 32),
		config.EnvPostgresDSN:      "postgres://u:p@127.0.0.1:5432/db",
		config.EnvEthRPCURL:        "",
		config.EnvChainID:          "",
		config.EnvContractAddress:  "0x" + strings.Repeat("1", 40),
		config.EnvEVMPrivateKeyHex: strings.Repeat("cd", 32),
		config.EnvCAPrivateKeyPath: write("ca.sk"),
		config.EnvCAPublicKeyPath:  write("ca.pk"),
		config.EnvProducerRegistry: write("registry.json"),
		config.EnvProducerKeyDir:   keyDir,
	}
}

func apply(t *testing.T, env map[string]string) {
	t.Helper()
	all := []string{
		config.EnvAEADKeyHex, config.EnvPostgresDSN, config.EnvEthRPCURL,
		config.EnvChainID, config.EnvContractAddress, config.EnvEVMPrivateKeyHex,
		config.EnvCAPrivateKeyPath, config.EnvCAPublicKeyPath,
		config.EnvProducerRegistry, config.EnvProducerKeyDir,
	}
	for _, k := range all {
		old, had := os.LookupEnv(k)
		key := k
		t.Cleanup(func() {
			if had {
				os.Setenv(key, old)
			} else {
				os.Unsetenv(key)
			}
		})
		if v, ok := env[k]; ok && v != "" {
			os.Setenv(k, v)
		} else {
			os.Unsetenv(k)
		}
	}
}

func TestLoadValidConfigurationUsesQ46Defaults(t *testing.T) {
	apply(t, validEnv(t))
	c, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.EthRPCURL != config.DefaultEthRPCURL {
		t.Errorf("EthRPCURL = %q, want %q", c.EthRPCURL, config.DefaultEthRPCURL)
	}
	if c.ChainID != config.DefaultChainID {
		t.Errorf("ChainID = %d, want %d", c.ChainID, config.DefaultChainID)
	}
	if len(c.AEADKey) != config.AEADKeyLen {
		t.Errorf("AEAD key length = %d, want %d", len(c.AEADKey), config.AEADKeyLen)
	}
}

// Q46: the program must never silently generate a replacement secret.
func TestMissingAEADKeyIsAStartupError(t *testing.T) {
	env := validEnv(t)
	env[config.EnvAEADKeyHex] = ""
	apply(t, env)
	c, err := config.Load()
	if err == nil {
		t.Fatalf("Load succeeded with %s absent; got key %x", config.EnvAEADKeyHex, c.AEADKey)
	}
}

func TestWrongLengthAEADKeyIsAStartupError(t *testing.T) {
	for _, bad := range []string{
		strings.Repeat("ab", 16), // 32 hex characters
		strings.Repeat("ab", 48), // 96 hex characters
		strings.Repeat("zz", 32), // right length, not hexadecimal
	} {
		env := validEnv(t)
		env[config.EnvAEADKeyHex] = bad
		apply(t, env)
		if c, err := config.Load(); err == nil {
			t.Fatalf("Load accepted AEAD key %q and produced %x", bad, c.AEADKey)
		}
	}
}

func TestInvalidContractAddressIsAStartupError(t *testing.T) {
	for _, bad := range []string{"1234", "0x1234", strings.Repeat("1", 42)} {
		env := validEnv(t)
		env[config.EnvContractAddress] = bad
		apply(t, env)
		if _, err := config.Load(); err == nil {
			t.Fatalf("Load accepted contract address %q", bad)
		}
	}
}

func TestMissingRegistryIsAStartupError(t *testing.T) {
	env := validEnv(t)
	env[config.EnvProducerRegistry] = filepath.Join(t.TempDir(), "absent.json")
	apply(t, env)
	if _, err := config.Load(); err == nil {
		t.Fatal("Load accepted a missing producer registry")
	}
}

func TestNonNumericChainIDIsAStartupError(t *testing.T) {
	env := validEnv(t)
	env[config.EnvChainID] = "mainnet"
	apply(t, env)
	if _, err := config.Load(); err == nil {
		t.Fatal("Load accepted a non-numeric chain id")
	}
}

func TestEachRequiredVariableIsRequired(t *testing.T) {
	required := []string{
		config.EnvAEADKeyHex, config.EnvPostgresDSN, config.EnvContractAddress,
		config.EnvEVMPrivateKeyHex, config.EnvCAPrivateKeyPath,
		config.EnvCAPublicKeyPath, config.EnvProducerRegistry,
		config.EnvProducerKeyDir,
	}
	for _, name := range required {
		env := validEnv(t)
		env[name] = ""
		apply(t, env)
		if _, err := config.Load(); err == nil {
			t.Errorf("Load succeeded with %s absent", name)
		}
	}
}

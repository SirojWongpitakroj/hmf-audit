// Package config loads the ten environment variables that make up the entire
// configuration surface of the system (Q46).
//
// The loader fails fast on any missing or invalid value. It never generates a
// replacement secret, under any circumstance (Q46).
package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The exactly ten environment variables named by Q46. No other environment
// variable is read anywhere in this system.
const (
	EnvAEADKeyHex       = "DHT_AEAD_KEY_HEX"
	EnvPostgresDSN      = "DHT_POSTGRES_DSN"
	EnvEthRPCURL        = "DHT_ETH_RPC_URL"
	EnvChainID          = "DHT_CHAIN_ID"
	EnvContractAddress  = "DHT_CONTRACT_ADDRESS"
	EnvEVMPrivateKeyHex = "DHT_EVM_PRIVATE_KEY_HEX"
	EnvCAPrivateKeyPath = "DHT_CA_PRIVATE_KEY_PATH"
	EnvCAPublicKeyPath  = "DHT_CA_PUBLIC_KEY_PATH"
	EnvProducerRegistry = "DHT_PRODUCER_REGISTRY_PATH"
	EnvProducerKeyDir   = "DHT_PRODUCER_KEY_DIR"
)

// Defaults stated by Q46.
const (
	DefaultEthRPCURL = "http://127.0.0.1:8545"
	DefaultChainID   = uint64(31337)
)

// AEADKeyLen is the AEAD key length in bytes: one 256-bit system-wide key
// (Q2), supplied as exactly 64 hex characters (Q46).
const AEADKeyLen = 32

// Config is the loaded, validated configuration.
type Config struct {
	AEADKey              []byte
	PostgresDSN          string
	EthRPCURL            string
	ChainID              uint64
	ContractAddress      string
	EVMPrivateKeyHex     string
	CAPrivateKeyPath     string
	CAPublicKeyPath      string
	ProducerRegistryPath string
	ProducerKeyDir       string
}

// Load reads and validates the ten Q46 environment variables. Any missing or
// invalid value is a startup error; nothing is defaulted except the two values
// Q46 gives defaults for, and no secret is ever generated.
func Load() (*Config, error) {
	c := &Config{}

	keyHex, ok := os.LookupEnv(EnvAEADKeyHex)
	if !ok {
		return nil, fmt.Errorf("config: %s is not set; the AEAD key is never generated", EnvAEADKeyHex)
	}
	key, err := decodeHexKey(keyHex, AEADKeyLen)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", EnvAEADKeyHex, err)
	}
	c.AEADKey = key

	c.PostgresDSN, err = requireNonEmpty(EnvPostgresDSN)
	if err != nil {
		return nil, err
	}

	c.EthRPCURL = os.Getenv(EnvEthRPCURL)
	if c.EthRPCURL == "" {
		c.EthRPCURL = DefaultEthRPCURL
	}

	chainIDText := os.Getenv(EnvChainID)
	if chainIDText == "" {
		c.ChainID = DefaultChainID
	} else {
		id, err := strconv.ParseUint(chainIDText, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("config: %s: not an unsigned integer: %q", EnvChainID, chainIDText)
		}
		c.ChainID = id
	}

	addr, err := requireNonEmpty(EnvContractAddress)
	if err != nil {
		return nil, err
	}
	if err := validateEthAddress(addr); err != nil {
		return nil, fmt.Errorf("config: %s: %w", EnvContractAddress, err)
	}
	c.ContractAddress = addr

	pk, err := requireNonEmpty(EnvEVMPrivateKeyHex)
	if err != nil {
		return nil, err
	}
	if _, err := decodeHexKey(pk, 32); err != nil {
		return nil, fmt.Errorf("config: %s: %w", EnvEVMPrivateKeyHex, err)
	}
	c.EVMPrivateKeyHex = pk

	if c.CAPrivateKeyPath, err = requireExistingFile(EnvCAPrivateKeyPath); err != nil {
		return nil, err
	}
	if c.CAPublicKeyPath, err = requireExistingFile(EnvCAPublicKeyPath); err != nil {
		return nil, err
	}
	if c.ProducerRegistryPath, err = requireExistingFile(EnvProducerRegistry); err != nil {
		return nil, err
	}
	if c.ProducerKeyDir, err = requireExistingDir(EnvProducerKeyDir); err != nil {
		return nil, err
	}

	return c, nil
}

func requireNonEmpty(name string) (string, error) {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return "", fmt.Errorf("config: %s is not set", name)
	}
	return v, nil
}

func requireExistingFile(name string) (string, error) {
	v, err := requireNonEmpty(name)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(v)
	if err != nil {
		return "", fmt.Errorf("config: %s: %q: %w", name, v, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("config: %s: %q is a directory, not a file", name, v)
	}
	return v, nil
}

func requireExistingDir(name string) (string, error) {
	v, err := requireNonEmpty(name)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(v)
	if err != nil {
		return "", fmt.Errorf("config: %s: %q: %w", name, v, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("config: %s: %q is not a directory", name, v)
	}
	return v, nil
}

func decodeHexKey(s string, wantBytes int) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	if len(s) != wantBytes*2 {
		return nil, fmt.Errorf("expected exactly %d hex characters, got %d", wantBytes*2, len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, errors.New("not valid hexadecimal")
	}
	return b, nil
}

func validateEthAddress(a string) error {
	if !strings.HasPrefix(a, "0x") {
		return errors.New("address must begin with 0x")
	}
	if len(a) != 42 {
		return fmt.Errorf("address must be 42 characters, got %d", len(a))
	}
	if _, err := hex.DecodeString(a[2:]); err != nil {
		return errors.New("address is not valid hexadecimal")
	}
	return nil
}

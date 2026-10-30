package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port     string
	Env      string
	LogLevel string

	BasicAuthUser string
	BasicAuthPass string

	SolanaRPC string
	SolanaWS  string
	ProgramID string

	DatabaseURL   string
	RedisAddr     string
	RedisPassword string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Port:          getenv("PORT", "8080"),
		Env:           getenv("APP_ENV", "development"),
		LogLevel:      getenv("LOG_LEVEL", "info"),
		BasicAuthUser: getenv("BASIC_AUTH_USER", "admin"),
		BasicAuthPass: getenv("BASIC_AUTH_PASS", ""),
		SolanaRPC:     getenv("SOLANA_RPC", "https://api.devnet.solana.com"),
		SolanaWS:      getenv("SOLANA_WS", "wss://api.devnet.solana.com"),
		ProgramID:     getenv("PROGRAM_ID", ""),
		DatabaseURL:   getenv("DATABASE_URL", ""),
		RedisAddr:     getenv("REDIS_ADDR", ""),
		RedisPassword: getenv("REDIS_PASSWORD", ""),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Port == "" {
		return fmt.Errorf("PORT is required")
	}
	if c.ProgramID == "" {
		return fmt.Errorf("PROGRAM_ID (Solana program pubkey) is required")
	}
	if c.BasicAuthPass == "" {
		return fmt.Errorf("BASIC_AUTH_PASS is required")
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

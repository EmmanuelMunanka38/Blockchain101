package main

import (
	"context"
	"log"

	"blockchain101-api/config"
	"blockchain101-api/internal/auth"
	"blockchain101-api/internal/database"
	"blockchain101-api/internal/handler"
	"blockchain101-api/internal/solana"
	"blockchain101-api/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	log := logger.New(cfg.LogLevel)

	programID, err := solana.ProgramID(cfg.ProgramID)
	if err != nil {
		log.Error("invalid PROGRAM_ID", "error", err)
		return
	}

	rpcClient := solana.NewRPC(cfg.SolanaRPC)

	ctx := context.Background()
	db, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("failed to connect to Postgres", "error", err)
	}
	if db != nil {
		defer db.Close()
	}

	rdb := database.NewRedis(cfg.RedisAddr, cfg.RedisPassword)
	if rdb != nil {
		defer rdb.Close()
	}

	srv := handler.New(cfg, log, rpcClient, programID, db, rdb)
	engine := srv.Router(auth.BasicAuth)

	log.Info("starting server", "port", cfg.Port, "env", cfg.Env, "rpc", cfg.SolanaRPC)
	if err := engine.Run(":" + cfg.Port); err != nil {
		log.Error("server error", "error", err)
	}
}

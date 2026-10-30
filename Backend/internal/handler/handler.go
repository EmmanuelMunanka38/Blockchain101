package handler

import (
	"log/slog"
	"net/http"

	solanago "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"blockchain101-api/config"
)

type Server struct {
	cfg       *config.Config
	logger    *slog.Logger
	rpc       *rpc.Client
	programID solanago.PublicKey
	db        *pgxpool.Pool
	rdb       *redis.Client
}

func New(cfg *config.Config, logger *slog.Logger, rpc *rpc.Client, programID solanago.PublicKey, db *pgxpool.Pool, rdb *redis.Client) *Server {
	return &Server{
		cfg:       cfg,
		logger:    logger,
		rpc:       rpc,
		programID: programID,
		db:        db,
		rdb:       rdb,
	}
}

func (s *Server) Router(auth func(string, string) gin.HandlerFunc) *gin.Engine {
	r := gin.Default()

	r.GET("/health", s.health)

	protected := r.Group("/api")
	protected.Use(auth(s.cfg.BasicAuthUser, s.cfg.BasicAuthPass))
	{
		protected.POST("/invoices", s.createInvoice)
		protected.GET("/invoices/status", s.invoiceStatus)
	}

	return r
}

func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "env": s.cfg.Env})
}

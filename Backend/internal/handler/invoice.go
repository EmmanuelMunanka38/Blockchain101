package handler

import (
	"net/http"
	"strconv"

	solanago "github.com/gagliardetto/solana-go"
	"github.com/gin-gonic/gin"

	"blockchain101-api/internal/solana"
)

type createInvoiceReq struct {
	Merchant  string `json:"merchant" binding:"required"`
	InvoiceID uint64 `json:"invoice_id" binding:"required"`
	Amount    uint64 `json:"amount" binding:"required"` // lamports
	Memo      string `json:"memo"`
}

func solanagoPubKey(s string) (solanago.PublicKey, error) {
	return solanago.PublicKeyFromBase58(s)
}

func (s *Server) createInvoice(c *gin.Context) {
	var req createInvoiceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	merchant, err := solanagoPubKey(req.Merchant)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid merchant public key"})
		return
	}

	pda, err := solana.InvoicePDA(s.programID, merchant, req.InvoiceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to derive invoice PDA"})
		return
	}

	if s.db != nil {
		_, err := s.db.Exec(c.Request.Context(),
			`INSERT INTO invoices (merchant, invoice_id, amount, memo, status)
			 VALUES ($1, $2, $3, $4, 'pending')
			 ON CONFLICT (merchant, invoice_id) DO NOTHING`,
			req.Merchant, req.InvoiceID, req.Amount, req.Memo)
		if err != nil {
			s.logger.Error("failed to store invoice", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"merchant":   req.Merchant,
		"invoice_id": req.InvoiceID,
		"pda":        pda.String(),
		"amount":     req.Amount,
		"status":     "pending",
	})
}

func (s *Server) invoiceStatus(c *gin.Context) {
	merchant, err := solanagoPubKey(c.Query("merchant"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid merchant public key"})
		return
	}

	invoiceID, err := strconv.ParseUint(c.Query("invoice_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid invoice_id"})
		return
	}

	pda, err := solana.InvoicePDA(s.programID, merchant, invoiceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to derive invoice PDA"})
		return
	}

	status, err := solana.ReadInvoiceStatus(c.Request.Context(), s.rpc, pda)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"pda":    pda.String(),
		"status": solana.StatusName(status),
	})
}

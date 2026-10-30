package solana

import (
	"context"
	"encoding/binary"
	"errors"

	solanago "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

const (
	StatusPending   uint8 = 0
	StatusPaid      uint8 = 1
	StatusCancelled uint8 = 2
)

// StatusName maps an invoice status byte to a human-readable string.
func StatusName(s uint8) string {
	switch s {
	case StatusPaid:
		return "paid"
	case StatusCancelled:
		return "cancelled"
	case StatusPending:
		return "pending"
	default:
		return "unknown"
	}
}

// NewRPC returns a Solana RPC client for the given endpoint.
func NewRPC(endpoint string) *rpc.Client {
	return rpc.New(endpoint)
}

// ProgramID parses a base58 program id into a PublicKey.
func ProgramID(s string) (solanago.PublicKey, error) {
	return solanago.PublicKeyFromBase58(s)
}

// InvoicePDA derives the invoice account address from the same seeds the
// on-chain program uses: ["invoice", merchant, invoice_id (u64 LE)].
func InvoicePDA(programID, merchant solanago.PublicKey, invoiceID uint64) (solanago.PublicKey, error) {
	idBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(idBytes, invoiceID)

	pda, _, err := solanago.FindProgramAddress(
		[][]byte{[]byte("invoice"), merchant.Bytes(), idBytes},
		programID,
	)
	return pda, err
}

// ReadInvoiceStatus fetches the invoice account and returns its status enum
// value. The `status` field lives at byte offset 64
// (8 discriminator + 32 merchant + 8 id + 8 amount + 8 expires_at).
func ReadInvoiceStatus(ctx context.Context, c *rpc.Client, pda solanago.PublicKey) (uint8, error) {
	acc, err := c.GetAccountInfoWithOpts(ctx, pda, &rpc.GetAccountInfoOpts{
		Commitment: rpc.CommitmentFinalized,
	})
	if err != nil {
		return 0, err
	}
	if acc == nil || acc.Value == nil {
		return 0, errors.New("invoice account not found")
	}

	data := acc.Value.Data.GetBinary()
	if len(data) < 65 {
		return 0, errors.New("invoice account data too short")
	}
	return data[64], nil
}

# Blockchain101

> My first blockchain app: a Solana wallet and payment app that lets people accept crypto payments online. Built to learn smart contracts (Anchor), Solana, and Go.

**Network:** Solana Devnet (mainnet only after serious testing)

---

## What it does

- Create / connect a Solana wallet
- Merchant creates an **invoice** stored on-chain by a smart contract
- Customer pays the invoice through the contract
- Backend detects the payment and updates the merchant dashboard
- Merchant gets notified when the invoice is paid

---

## Tech Stack

| Part | Tech |
|---|---|
| Smart contract | Rust + Anchor |
| Backend | Go (`solana-go`) |
| Frontend | React + Solana Wallet Adapter |
| Database | PostgreSQL |
| Cache / dedupe | Redis |
| Containers | Docker + Docker Compose |
| Network | Solana Devnet |

---

## Setup

### Install

```bash
# Rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

# Solana CLI
sh -c "$(curl -sSfL https://release.anza.xyz/stable/install)"

# Anchor (via avm)
cargo install --git https://github.com/coral-xyz/anchor avm --force
avm install latest && avm use latest

# Go 1.22+, Node 20+, Docker
```

### Devnet wallet

```bash
solana config set --url devnet
solana-keygen new
solana airdrop 2
solana balance
```

### Create and build the program

```bash
anchor init blockchain101
cd blockchain101
anchor build
anchor keys sync     # puts the real program ID into lib.rs and Anchor.toml
anchor test
anchor deploy
```

---

## Smart Contract (Anchor / Rust)

The program lets a merchant create an invoice (a PDA account), lets a customer pay it in SOL, and lets the merchant cancel it if it's still unpaid.

`programs/blockchain101/src/lib.rs`

```rust
use anchor_lang::prelude::*;
use anchor_lang::system_program;

declare_id!("11111111111111111111111111111111"); // replaced by `anchor keys sync`

#[program]
pub mod blockchain101 {
    use super::*;

    pub fn create_invoice(
        ctx: Context<CreateInvoice>,
        invoice_id: u64,
        amount: u64,       // in lamports (1 SOL = 1_000_000_000)
        expires_at: i64,   // unix timestamp
        memo: String,
    ) -> Result<()> {
        require!(amount > 0, InvoiceError::InvalidAmount);
        require!(memo.len() <= 64, InvoiceError::MemoTooLong);

        let now = Clock::get()?.unix_timestamp;
        require!(expires_at > now, InvoiceError::InvalidExpiry);

        let invoice = &mut ctx.accounts.invoice;
        invoice.merchant = ctx.accounts.merchant.key();
        invoice.invoice_id = invoice_id;
        invoice.amount = amount;
        invoice.expires_at = expires_at;
        invoice.status = InvoiceStatus::Pending;
        invoice.payer = Pubkey::default();
        invoice.paid_at = 0;
        invoice.memo = memo;
        invoice.bump = ctx.bumps.invoice;

        emit!(InvoiceCreated {
            merchant: invoice.merchant,
            invoice_id,
            amount,
        });
        Ok(())
    }

    pub fn pay_invoice(ctx: Context<PayInvoice>) -> Result<()> {
        let now = Clock::get()?.unix_timestamp;
        let amount = ctx.accounts.invoice.amount;

        require!(
            ctx.accounts.invoice.status == InvoiceStatus::Pending,
            InvoiceError::NotPending
        );
        require!(now <= ctx.accounts.invoice.expires_at, InvoiceError::Expired);

        // Move SOL from customer to merchant (CPI to the System Program)
        system_program::transfer(
            CpiContext::new(
                ctx.accounts.system_program.to_account_info(),
                system_program::Transfer {
                    from: ctx.accounts.payer.to_account_info(),
                    to: ctx.accounts.merchant.to_account_info(),
                },
            ),
            amount,
        )?;

        let invoice = &mut ctx.accounts.invoice;
        invoice.status = InvoiceStatus::Paid;
        invoice.payer = ctx.accounts.payer.key();
        invoice.paid_at = now;

        emit!(InvoicePaid {
            merchant: invoice.merchant,
            invoice_id: invoice.invoice_id,
            payer: invoice.payer,
            amount,
        });
        Ok(())
    }

    pub fn cancel_invoice(ctx: Context<CancelInvoice>) -> Result<()> {
        let invoice = &mut ctx.accounts.invoice;
        require!(invoice.status == InvoiceStatus::Pending, InvoiceError::NotPending);
        invoice.status = InvoiceStatus::Cancelled;
        Ok(())
    }
}

// ---------- Accounts ----------

#[derive(Accounts)]
#[instruction(invoice_id: u64)]
pub struct CreateInvoice<'info> {
    #[account(
        init,
        payer = merchant,
        space = 8 + Invoice::INIT_SPACE,
        seeds = [b"invoice", merchant.key().as_ref(), &invoice_id.to_le_bytes()],
        bump
    )]
    pub invoice: Account<'info, Invoice>,
    #[account(mut)]
    pub merchant: Signer<'info>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct PayInvoice<'info> {
    #[account(
        mut,
        seeds = [b"invoice", invoice.merchant.as_ref(), &invoice.invoice_id.to_le_bytes()],
        bump = invoice.bump
    )]
    pub invoice: Account<'info, Invoice>,
    #[account(mut)]
    pub payer: Signer<'info>,
    /// CHECK: must equal invoice.merchant, enforced by the `address` constraint
    #[account(mut, address = invoice.merchant)]
    pub merchant: UncheckedAccount<'info>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct CancelInvoice<'info> {
    #[account(mut, has_one = merchant)]
    pub invoice: Account<'info, Invoice>,
    pub merchant: Signer<'info>,
}

// ---------- State ----------
// Field order matters: the Go backend reads `status` at byte offset 64.

#[account]
#[derive(InitSpace)]
pub struct Invoice {
    pub merchant: Pubkey,        // 32
    pub invoice_id: u64,         // 8
    pub amount: u64,             // 8
    pub expires_at: i64,         // 8
    pub status: InvoiceStatus,   // 1  <- offset 8 (discriminator) + 56 = 64
    pub payer: Pubkey,           // 32
    pub paid_at: i64,            // 8
    #[max_len(64)]
    pub memo: String,            // 4 + up to 64
    pub bump: u8,                // 1
}

#[derive(AnchorSerialize, AnchorDeserialize, Clone, Copy, PartialEq, Eq, InitSpace)]
pub enum InvoiceStatus {
    Pending,    // 0
    Paid,       // 1
    Cancelled,  // 2
}

// ---------- Events ----------

#[event]
pub struct InvoiceCreated {
    pub merchant: Pubkey,
    pub invoice_id: u64,
    pub amount: u64,
}

#[event]
pub struct InvoicePaid {
    pub merchant: Pubkey,
    pub invoice_id: u64,
    pub payer: Pubkey,
    pub amount: u64,
}

// ---------- Errors ----------

#[error_code]
pub enum InvoiceError {
    #[msg("Amount must be greater than zero")]
    InvalidAmount,
    #[msg("Memo is too long (max 64 bytes)")]
    MemoTooLong,
    #[msg("Expiry must be in the future")]
    InvalidExpiry,
    #[msg("Invoice is not pending")]
    NotPending,
    #[msg("Invoice has expired")]
    Expired,
}
```

### Contract tests (TypeScript)

`tests/blockchain101.ts`

```ts
import * as anchor from "@coral-xyz/anchor";
import { Program, BN } from "@coral-xyz/anchor";
import { Blockchain101 } from "../target/types/blockchain101";
import { Keypair, LAMPORTS_PER_SOL, PublicKey, SystemProgram } from "@solana/web3.js";
import { assert } from "chai";

describe("blockchain101", () => {
  const provider = anchor.AnchorProvider.env();
  anchor.setProvider(provider);
  const program = anchor.workspace.Blockchain101 as Program<Blockchain101>;

  const merchant = provider.wallet as anchor.Wallet;
  const customer = Keypair.generate();
  const invoiceId = new BN(1);

  const [invoicePda] = PublicKey.findProgramAddressSync(
    [
      Buffer.from("invoice"),
      merchant.publicKey.toBuffer(),
      invoiceId.toArrayLike(Buffer, "le", 8),
    ],
    program.programId
  );

  before(async () => {
    const sig = await provider.connection.requestAirdrop(customer.publicKey, 2 * LAMPORTS_PER_SOL);
    await provider.connection.confirmTransaction(sig);
  });

  it("creates an invoice", async () => {
    const expiresAt = new BN(Math.floor(Date.now() / 1000) + 3600);

    await program.methods
      .createInvoice(invoiceId, new BN(0.5 * LAMPORTS_PER_SOL), expiresAt, "Order #1")
      .accounts({ invoice: invoicePda, merchant: merchant.publicKey })
      .rpc();

    const invoice = await program.account.invoice.fetch(invoicePda);
    assert.equal(invoice.amount.toNumber(), 0.5 * LAMPORTS_PER_SOL);
    assert.deepEqual(invoice.status, { pending: {} });
  });

  it("customer pays the invoice", async () => {
    const before = await provider.connection.getBalance(merchant.publicKey);

    await program.methods
      .payInvoice()
      .accountsPartial({
        invoice: invoicePda,
        payer: customer.publicKey,
        merchant: merchant.publicKey,
        systemProgram: SystemProgram.programId,
      })
      .signers([customer])
      .rpc();

    const after = await provider.connection.getBalance(merchant.publicKey);
    assert.equal(after - before, 0.5 * LAMPORTS_PER_SOL);

    const invoice = await program.account.invoice.fetch(invoicePda);
    assert.deepEqual(invoice.status, { paid: {} });
  });

  it("cannot pay twice", async () => {
    try {
      await program.methods
        .payInvoice()
        .accountsPartial({
          invoice: invoicePda,
          payer: customer.publicKey,
          merchant: merchant.publicKey,
        })
        .signers([customer])
        .rpc();
      assert.fail("should have thrown");
    } catch (e: any) {
      assert.include(e.toString(), "NotPending");
    }
  });
});
```

---

## Backend (Go)

```bash
go mod init blockchain101-api
go get github.com/gagliardetto/solana-go
go get github.com/jackc/pgx/v5
go get github.com/redis/go-redis/v9
```

### Wallet and balance

```go
package main

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

func main() {
	ctx := context.Background()
	client := rpc.New(rpc.DevNet_RPC)

	// Generate a new keypair
	wallet := solana.NewWallet()
	fmt.Println("Public key:", wallet.PublicKey())

	// Read balance (lamports -> SOL)
	out, err := client.GetBalance(ctx, wallet.PublicKey(), rpc.CommitmentFinalized)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Balance: %f SOL\n", float64(out.Value)/float64(solana.LAMPORTS_PER_SOL))
}
```

### Find the invoice PDA and read its status

Matches the seeds used in the contract: `["invoice", merchant, invoice_id as u64 little-endian]`.

```go
package main

import (
	"context"
	"encoding/binary"
	"errors"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

const (
	StatusPending   uint8 = 0
	StatusPaid      uint8 = 1
	StatusCancelled uint8 = 2
)

func invoicePDA(programID, merchant solana.PublicKey, invoiceID uint64) (solana.PublicKey, error) {
	idBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(idBytes, invoiceID)

	pda, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("invoice"), merchant.Bytes(), idBytes},
		programID,
	)
	return pda, err
}

// Layout: 8 discriminator + 32 merchant + 8 id + 8 amount + 8 expires = status at 64
func readInvoiceStatus(ctx context.Context, c *rpc.Client, pda solana.PublicKey) (uint8, error) {
	acc, err := c.GetAccountInfoWithOpts(ctx, pda, &rpc.GetAccountInfoOpts{
		Commitment: rpc.CommitmentFinalized,
	})
	if err != nil {
		return 0, err
	}
	data := acc.Value.Data.GetBinary()
	if len(data) < 65 {
		return 0, errors.New("invoice account data too short")
	}
	return data[64], nil
}
```

### Payment listener (WebSocket + Redis dedupe)

Subscribes to every transaction that touches the program, ignores duplicates with Redis, then checks pending invoices on-chain.

```go
package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/ws"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func listen(
	ctx context.Context,
	programID solana.PublicKey,
	rpcClient *rpc.Client,
	db *pgxpool.Pool,
	rdb *redis.Client,
) error {
	wsClient, err := ws.Connect(ctx, rpc.DevNet_WS)
	if err != nil {
		return err
	}
	defer wsClient.Close()

	sub, err := wsClient.LogsSubscribeMentions(programID, rpc.CommitmentConfirmed)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	log.Println("Listening for program logs...")

	for {
		msg, err := sub.Recv(ctx)
		if err != nil {
			return err
		}
		if msg.Value.Err != nil {
			continue // failed transaction
		}

		sig := msg.Value.Signature.String()

		// Redis: only process each signature once
		isNew, err := rdb.SetNX(ctx, "seen:"+sig, 1, 24*time.Hour).Result()
		if err != nil || !isNew {
			continue
		}

		for _, line := range msg.Value.Logs {
			if strings.Contains(line, "Instruction: PayInvoice") {
				go func() {
					if err := reconcilePending(ctx, rpcClient, db, programID); err != nil {
						log.Println("reconcile error:", err)
					}
				}()
				break
			}
		}
	}
}

type pendingInvoice struct {
	ID        int64
	Merchant  string
	InvoiceID uint64
}

func reconcilePending(
	ctx context.Context,
	c *rpc.Client,
	db *pgxpool.Pool,
	programID solana.PublicKey,
) error {
	rows, err := db.Query(ctx,
		`SELECT id, merchant, invoice_id FROM invoices WHERE status = 'pending'`)
	if err != nil {
		return err
	}

	var pending []pendingInvoice
	for rows.Next() {
		var p pendingInvoice
		if err := rows.Scan(&p.ID, &p.Merchant, &p.InvoiceID); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, p)
	}
	rows.Close()

	for _, p := range pending {
		merchant, err := solana.PublicKeyFromBase58(p.Merchant)
		if err != nil {
			continue
		}
		pda, err := invoicePDA(programID, merchant, p.InvoiceID)
		if err != nil {
			continue
		}
		status, err := readInvoiceStatus(ctx, c, pda)
		if err != nil {
			continue
		}
		if status == StatusPaid {
			_, err := db.Exec(ctx,
				`UPDATE invoices SET status = 'paid', paid_at = now() WHERE id = $1`, p.ID)
			if err != nil {
				return err
			}
			log.Printf("Invoice %d marked as paid", p.ID)
			// TODO: send webhook / email to merchant
		}
	}
	return nil
}
```

### Simple HTTP endpoint

```go
package main

import (
	"encoding/json"
	"net/http"
)

type createInvoiceReq struct {
	Merchant  string `json:"merchant"`
	InvoiceID uint64 `json:"invoice_id"`
	Amount    uint64 `json:"amount"` // lamports
	Memo      string `json:"memo"`
}

func (s *Server) handleCreateInvoice(w http.ResponseWriter, r *http.Request) {
	var req createInvoiceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	_, err := s.db.Exec(r.Context(),
		`INSERT INTO invoices (merchant, invoice_id, amount, memo, status)
		 VALUES ($1, $2, $3, $4, 'pending')`,
		req.Merchant, req.InvoiceID, req.Amount, req.Memo)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
```

---

## Database (PostgreSQL)

```sql
CREATE TABLE invoices (
  id          BIGSERIAL PRIMARY KEY,
  merchant    TEXT   NOT NULL,          -- base58 public key
  invoice_id  BIGINT NOT NULL,          -- same id used for the on-chain PDA
  amount      BIGINT NOT NULL,          -- lamports
  memo        TEXT,
  status      TEXT   NOT NULL DEFAULT 'pending', -- pending | paid | cancelled | expired
  created_at  TIMESTAMPTZ DEFAULT now(),
  paid_at     TIMESTAMPTZ,
  UNIQUE (merchant, invoice_id)
);

CREATE INDEX idx_invoices_status ON invoices(status);
```

---

## Frontend (React)

```bash
npm create vite@latest web -- --template react-ts
cd web
npm i @solana/web3.js @coral-xyz/anchor \
      @solana/wallet-adapter-react @solana/wallet-adapter-react-ui \
      @solana/wallet-adapter-wallets @solana/wallet-adapter-base
```

### Wallet providers

`src/main.tsx`

```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import { ConnectionProvider, WalletProvider } from "@solana/wallet-adapter-react";
import { WalletModalProvider } from "@solana/wallet-adapter-react-ui";
import { PhantomWalletAdapter, SolflareWalletAdapter } from "@solana/wallet-adapter-wallets";
import { clusterApiUrl } from "@solana/web3.js";
import "@solana/wallet-adapter-react-ui/styles.css";
import App from "./App";

const wallets = [new PhantomWalletAdapter(), new SolflareWalletAdapter()];

ReactDOM.createRoot(document.getElementById("root")!).render(
  <ConnectionProvider endpoint={clusterApiUrl("devnet")}>
    <WalletProvider wallets={wallets} autoConnect>
      <WalletModalProvider>
        <App />
      </WalletModalProvider>
    </WalletProvider>
  </ConnectionProvider>
);
```

### Connect button + balance

`src/Balance.tsx`

```tsx
import { useEffect, useState } from "react";
import { useConnection, useWallet } from "@solana/wallet-adapter-react";
import { WalletMultiButton } from "@solana/wallet-adapter-react-ui";
import { LAMPORTS_PER_SOL } from "@solana/web3.js";

export default function Balance() {
  const { connection } = useConnection();
  const { publicKey } = useWallet();
  const [balance, setBalance] = useState<number | null>(null);

  useEffect(() => {
    if (!publicKey) return;
    connection.getBalance(publicKey).then((l) => setBalance(l / LAMPORTS_PER_SOL));
  }, [publicKey, connection]);

  return (
    <div>
      <WalletMultiButton />
      {publicKey && <p>{publicKey.toBase58()}</p>}
      {balance !== null && <p>{balance.toFixed(4)} SOL</p>}
    </div>
  );
}
```

### Anchor program hook

`src/useProgram.ts`

```ts
import { useMemo } from "react";
import { useAnchorWallet, useConnection } from "@solana/wallet-adapter-react";
import { AnchorProvider, Program } from "@coral-xyz/anchor";
import idl from "./idl/blockchain101.json"; // copy from target/idl/blockchain101.json

export function useProgram() {
  const { connection } = useConnection();
  const wallet = useAnchorWallet();

  return useMemo(() => {
    if (!wallet) return null;
    const provider = new AnchorProvider(connection, wallet, { commitment: "confirmed" });
    return new Program(idl as any, provider);
  }, [connection, wallet]);
}
```

### Create an invoice (merchant)

`src/CreateInvoice.tsx`

```tsx
import { useState } from "react";
import { BN } from "@coral-xyz/anchor";
import { LAMPORTS_PER_SOL, PublicKey } from "@solana/web3.js";
import { useWallet } from "@solana/wallet-adapter-react";
import { useProgram } from "./useProgram";

export function getInvoicePda(programId: PublicKey, merchant: PublicKey, invoiceId: BN) {
  return PublicKey.findProgramAddressSync(
    [Buffer.from("invoice"), merchant.toBuffer(), invoiceId.toArrayLike(Buffer, "le", 8)],
    programId
  )[0];
}

export default function CreateInvoice() {
  const program = useProgram();
  const { publicKey } = useWallet();
  const [amount, setAmount] = useState("0.1");
  const [memo, setMemo] = useState("");
  const [link, setLink] = useState("");

  const create = async () => {
    if (!program || !publicKey) return;

    const invoiceId = new BN(Date.now());
    const expiresAt = new BN(Math.floor(Date.now() / 1000) + 60 * 60);
    const invoice = getInvoicePda(program.programId, publicKey, invoiceId);

    await program.methods
      .createInvoice(invoiceId, new BN(parseFloat(amount) * LAMPORTS_PER_SOL), expiresAt, memo)
      .accounts({ invoice, merchant: publicKey })
      .rpc();

    // Also save it in the backend so the dashboard can track it
    await fetch("/api/invoices", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        merchant: publicKey.toBase58(),
        invoice_id: invoiceId.toNumber(),
        amount: Math.round(parseFloat(amount) * LAMPORTS_PER_SOL),
        memo,
      }),
    });

    setLink(`${window.location.origin}/pay/${invoice.toBase58()}`);
  };

  return (
    <div>
      <input value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="Amount (SOL)" />
      <input value={memo} onChange={(e) => setMemo(e.target.value)} placeholder="Memo" />
      <button onClick={create} disabled={!publicKey}>Create invoice</button>
      {link && <p>Share this link: {link}</p>}
    </div>
  );
}
```

### Pay an invoice (customer)

`src/PayInvoice.tsx`

```tsx
import { useState } from "react";
import { PublicKey, SystemProgram } from "@solana/web3.js";
import { useWallet } from "@solana/wallet-adapter-react";
import { useProgram } from "./useProgram";

export default function PayInvoice({ invoiceAddress }: { invoiceAddress: string }) {
  const program = useProgram();
  const { publicKey } = useWallet();
  const [status, setStatus] = useState("");

  const pay = async () => {
    if (!program || !publicKey) return;
    try {
      setStatus("Sending...");
      const invoicePk = new PublicKey(invoiceAddress);
      const invoice: any = await (program.account as any).invoice.fetch(invoicePk);

      const sig = await program.methods
        .payInvoice()
        .accountsPartial({
          invoice: invoicePk,
          payer: publicKey,
          merchant: invoice.merchant,
          systemProgram: SystemProgram.programId,
        })
        .rpc();

      setStatus(`Paid! Tx: ${sig}`);
    } catch (e: any) {
      setStatus(`Failed: ${e.message}`);
    }
  };

  return (
    <div>
      <button onClick={pay} disabled={!publicKey}>Pay with Solana</button>
      <p>{status}</p>
    </div>
  );
}
```

### Optional: Solana Pay QR (plain SOL transfer)

A simple way to learn payment requests before using your own contract.

```bash
npm i @solana/pay bignumber.js
```

```ts
import { encodeURL, createQR } from "@solana/pay";
import { Keypair, PublicKey } from "@solana/web3.js";
import BigNumber from "bignumber.js";

const reference = Keypair.generate().publicKey; // lets you find the tx later

const url = encodeURL({
  recipient: new PublicKey("MERCHANT_WALLET_ADDRESS"),
  amount: new BigNumber(0.1),
  reference,
  label: "Blockchain101",
  message: "Order #1",
});

const qr = createQR(url, 300);
qr.append(document.getElementById("qr")!);
```

---

## Docker

`docker-compose.yml`

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: b101
      POSTGRES_PASSWORD: b101pass
      POSTGRES_DB: blockchain101
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

  redis:
    image: redis:7
    ports:
      - "6379:6379"

  api:
    build: ./api
    environment:
      DATABASE_URL: postgres://b101:b101pass@postgres:5432/blockchain101
      REDIS_ADDR: redis:6379
      SOLANA_RPC: https://api.devnet.solana.com
      SOLANA_WS: wss://api.devnet.solana.com
      PROGRAM_ID: YOUR_PROGRAM_ID
    depends_on:
      - postgres
      - redis
    ports:
      - "8080:8080"

  web:
    build: ./web
    ports:
      - "5173:80"
    depends_on:
      - api

volumes:
  pgdata:
```

`api/Dockerfile`

```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o server .

FROM alpine:3.20
WORKDIR /app
COPY --from=build /app/server .
EXPOSE 8080
CMD ["./server"]
```

`web/Dockerfile`

```dockerfile
FROM node:20-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
```

Run everything:

```bash
docker compose up --build
```

---

## Smart Contract Concepts I'm Learning

- **Accounts model:** everything on Solana is an account; programs are stateless
- **PDAs (Program Derived Addresses):** deterministic addresses owned by the program (used for invoices)
- **CPI (Cross-Program Invocation):** how my program calls the System Program to move SOL
- **Rent and account space:** why `space = 8 + Invoice::INIT_SPACE`
- **Signers and constraints:** `has_one`, `address`, `seeds`, `bump`
- **Events and logs:** how the Go backend knows something happened
- **Lamports:** 1 SOL = 1,000,000,000 lamports

---

## Security Checklist

- [ ] Private keys never touch the backend
- [ ] Check every account constraint in the contract (merchant address, signer, status)
- [ ] Prevent double payment (status check) and expired payments
- [ ] Use `finalized` commitment before treating a payment as final
- [ ] Never trust the frontend: the backend verifies payments on-chain
- [ ] Test every failure case in `anchor test`
- [ ] Devnet only until the contract has been reviewed

---

## Roadmap

1. [ ] Hello-world Anchor program, deploy to devnet
2. [ ] Invoice program: create, pay, cancel + tests
3. [ ] Go backend: wallet, balance, PDA reading
4. [ ] Payment listener with Redis and Postgres
5. [ ] React UI: connect wallet, create invoice, pay invoice
6. [ ] Docker Compose for the whole app
7. [ ] USDC (SPL token) payments
8. [ ] Webhooks and merchant dashboard
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	ErrAccountNotFound   = errors.New("account not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

const lastTxLimit = 10

type Store interface {
	GetBalance(ctx context.Context, accountID int64) (BalanceResponse, error)
	ApplyTransaction(ctx context.Context, accountID int64, req TxRequest) (int64, error)
	Ping(ctx context.Context) error
	Close() error
}

type pgStore struct {
	db *sql.DB
}

func NewPgStore(dsn string) (*pgStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &pgStore{db: db}, nil
}

func (s *pgStore) Close() error { return s.db.Close() }

func (s *pgStore) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *pgStore) GetBalance(ctx context.Context, accountID int64) (BalanceResponse, error) {
	resp := BalanceResponse{AccountID: accountID, Transactions: []Transaction{}}

	err := s.db.QueryRowContext(ctx,
		`SELECT balance FROM accounts WHERE id = $1`, accountID).Scan(&resp.BalanceCents)
	if errors.Is(err, sql.ErrNoRows) {
		return resp, ErrAccountNotFound
	}
	if err != nil {
		return resp, fmt.Errorf("select balance: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, amount_cents, type, created_at
		   FROM transactions
		  WHERE account_id = $1
		  ORDER BY id DESC
		  LIMIT $2`,
		accountID, lastTxLimit)
	if err != nil {
		return resp, fmt.Errorf("select transactions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			t  Transaction
			ts time.Time
		)
		if err := rows.Scan(&t.ID, &t.AmountCents, &t.Type, &ts); err != nil {
			return resp, fmt.Errorf("scan transaction: %w", err)
		}
		t.CreatedAt = ts.UTC().Format(time.RFC3339)
		resp.Transactions = append(resp.Transactions, t)
	}
	return resp, rows.Err()
}

func (s *pgStore) ApplyTransaction(ctx context.Context, accountID int64, req TxRequest) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO accounts (id, balance) VALUES ($1, 0)
		 ON CONFLICT (id) DO NOTHING`, accountID); err != nil {
		return 0, fmt.Errorf("ensure account: %w", err)
	}

	var newBalance int64
	switch req.Type {
	case TxCredit:
		err = tx.QueryRowContext(ctx,
			`UPDATE accounts SET balance = balance + $1 WHERE id = $2 RETURNING balance`,
			req.AmountCents, accountID).Scan(&newBalance)
	case TxDebit:
		err = tx.QueryRowContext(ctx,
			`UPDATE accounts SET balance = balance - $1
			  WHERE id = $2 AND balance >= $1
			  RETURNING balance`,
			req.AmountCents, accountID).Scan(&newBalance)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInsufficientFunds
		}
	default:
		return 0, ErrInvalidType
	}
	if err != nil {
		return 0, fmt.Errorf("update balance: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO transactions (account_id, amount_cents, type)
		 VALUES ($1, $2, $3)`,
		accountID, req.AmountCents, string(req.Type)); err != nil {
		return 0, fmt.Errorf("insert transaction: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	return newBalance, nil
}

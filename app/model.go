package main

import (
	"errors"
	"strings"
)

type TxType string

const (
	TxCredit TxType = "credit"
	TxDebit  TxType = "debit"
)

type Transaction struct {
	ID          int64  `json:"id"`
	AmountCents int64  `json:"amount_cents"`
	Type        TxType `json:"type"`
	CreatedAt   string `json:"created_at"`
}

type BalanceResponse struct {
	AccountID    int64         `json:"account_id"`
	BalanceCents int64         `json:"balance_cents"`
	Transactions []Transaction `json:"transactions"`
}

type TxRequest struct {
	AmountCents int64  `json:"amount_cents"`
	Type        TxType `json:"type"`
}

var (
	ErrInvalidAmount = errors.New("amount_cents must be a positive integer")
	ErrInvalidType   = errors.New("type must be 'credit' or 'debit'")
)

func (r TxRequest) Normalize() TxRequest {
	return TxRequest{
		AmountCents: r.AmountCents,
		Type:        TxType(strings.ToLower(string(r.Type))),
	}
}

func (r TxRequest) Validate() error {
	if r.AmountCents <= 0 {
		return ErrInvalidAmount
	}
	switch r.Type {
	case TxCredit, TxDebit:
		return nil
	default:
		return ErrInvalidType
	}
}

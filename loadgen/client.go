package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	http   *http.Client
	target string
}

func NewClient(target string) *Client {
	return &Client{
		target: target,
		http: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        512,
				MaxIdleConnsPerHost: 512,
				IdleConnTimeout:     30 * time.Second,
			},
		},
	}
}

func (c *Client) getBalance(account int64) (int, error) {
	resp, err := c.http.Get(fmt.Sprintf("%s/accounts/%d/balance", c.target, account))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func (c *Client) postTransaction(account, amountCents int64, txType string) (int, error) {
	body := fmt.Sprintf(`{"amount_cents":%d,"type":%q}`, amountCents, txType)
	resp, err := c.http.Post(
		fmt.Sprintf("%s/accounts/%d/transactions", c.target, account),
		"application/json",
		bytes.NewBufferString(body),
	)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

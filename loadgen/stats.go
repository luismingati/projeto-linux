package main

import "sync/atomic"

type Stats struct {
	total     atomic.Int64
	ok2xx     atomic.Int64
	rej4xx    atomic.Int64
	err5xx    atomic.Int64
	transport atomic.Int64

	credits   atomic.Int64
	debits    atomic.Int64
	overdraft atomic.Int64
}

func (s *Stats) record(status int, transportErr bool) {
	s.total.Add(1)
	if transportErr {
		s.transport.Add(1)
		return
	}
	switch {
	case status >= 200 && status < 300:
		s.ok2xx.Add(1)
	case status == 422:
		s.rej4xx.Add(1)
		s.overdraft.Add(1)
	case status >= 400 && status < 500:
		s.rej4xx.Add(1)
	case status >= 500:
		s.err5xx.Add(1)
	}
}

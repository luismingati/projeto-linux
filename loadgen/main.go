package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"
)

const (
	readPct   = 35
	creditPct = 40
)

func main() {
	target := flag.String("target", "http://localhost:8080", "base URL of the Nginx load balancer")
	users := flag.Int64("users", 1000, "number of distinct accounts (ids 1..users)")
	workers := flag.Int("workers", 50, "concurrent virtual users (goroutines)")
	duration := flag.Duration("duration", 30*time.Second, "how long to generate load")
	rps := flag.Int("rps", 0, "global target requests/sec (0 = as fast as possible)")
	seed := flag.Bool("seed", true, "give every account a starting credit before the load phase")
	flag.Parse()

	if *users < 1 || *workers < 1 {
		log.Fatal("users and workers must both be >= 1")
	}

	client := NewClient(*target)
	stats := &Stats{}

	fmt.Printf("loadgen → target=%s  users=%d  workers=%d  duration=%s  rps=%d\n",
		*target, *users, *workers, *duration, *rps)

	if code, err := client.getBalance(1); err != nil {
		log.Fatalf("cannot reach target %s: %v", *target, err)
	} else {
		fmt.Printf("preflight ok (GET /accounts/1/balance → %d)\n", code)
	}

	if *seed {
		fmt.Printf("seeding %d accounts with a starting credit...\n", *users)
		t0 := time.Now()
		seedAccounts(client, *users, *workers)
		fmt.Printf("seeded in %s\n", time.Since(t0).Round(time.Millisecond))
	}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var limiter *time.Ticker
	if *rps > 0 {
		if interval := time.Second / time.Duration(*rps); interval > 0 {
			limiter = time.NewTicker(interval)
			defer limiter.Stop()
		}
	}

	fmt.Println("generating load... (Ctrl-C to stop early)")
	start := time.Now()

	repCtx, repCancel := context.WithCancel(context.Background())
	go reporter(repCtx, stats, start)

	latencies := make([][]time.Duration, *workers)
	var wg sync.WaitGroup
	for i := range *workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(idx)*7919))
			local := make([]time.Duration, 0, 4096)
			for {
				if ctx.Err() != nil {
					break
				}
				if limiter != nil {
					select {
					case <-ctx.Done():
						break
					case <-limiter.C:
					}
				}
				if ctx.Err() != nil {
					break
				}
				local = append(local, doOne(client, stats, rng, *users))
			}
			latencies[idx] = local
		}(i)
	}

	wg.Wait()
	repCancel()
	elapsed := time.Since(start)

	merged := make([]time.Duration, 0, *workers*4096)
	for _, l := range latencies {
		merged = append(merged, l...)
	}
	slices.Sort(merged)

	printSummary(stats, elapsed, merged)
}

func doOne(c *Client, s *Stats, rng *rand.Rand, users int64) time.Duration {
	account := rng.Int63n(users) + 1
	roll := rng.Intn(100)
	t0 := time.Now()

	var (
		status int
		err    error
	)
	switch {
	case roll < readPct:
		status, err = c.getBalance(account)
	case roll < readPct+creditPct:
		s.credits.Add(1)
		status, err = c.postTransaction(account, rng.Int63n(50000)+100, "credit")
	default:
		s.debits.Add(1)
		status, err = c.postTransaction(account, rng.Int63n(20000)+100, "debit")
	}

	lat := time.Since(t0)
	s.record(status, err != nil)
	return lat
}

func seedAccounts(c *Client, users int64, workers int) {
	ids := make(chan int64, 2048)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(idx)*104729))
			for id := range ids {
				c.postTransaction(id, rng.Int63n(90000)+10000, "credit")
			}
		}(i)
	}
	for id := int64(1); id <= users; id++ {
		ids <- id
	}
	close(ids)
	wg.Wait()
}

func reporter(ctx context.Context, s *Stats, start time.Time) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var lastTotal int64
	lastTime := start
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			total := s.total.Load()
			rps := float64(total-lastTotal) / now.Sub(lastTime).Seconds()
			fmt.Printf("[%5.0fs] total=%-8d rps=%-7.0f 2xx=%-8d 4xx=%-7d (overdraft=%-6d) 5xx=%-4d errs=%d\n",
				now.Sub(start).Seconds(), total, rps,
				s.ok2xx.Load(), s.rej4xx.Load(), s.overdraft.Load(), s.err5xx.Load(), s.transport.Load())
			lastTotal = total
			lastTime = now
		}
	}
}

func printSummary(s *Stats, elapsed time.Duration, lat []time.Duration) {
	total := s.total.Load()
	secs := elapsed.Seconds()
	fmt.Println("\n──────── summary ────────")
	fmt.Printf("elapsed:         %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("requests:        %d  (%.0f req/s)\n", total, float64(total)/secs)
	fmt.Printf("  2xx success:   %d\n", s.ok2xx.Load())
	fmt.Printf("  4xx rejected:  %d  (overdraft 422: %d)\n", s.rej4xx.Load(), s.overdraft.Load())
	fmt.Printf("  5xx errors:    %d\n", s.err5xx.Load())
	fmt.Printf("  transport err: %d\n", s.transport.Load())
	fmt.Printf("ops:             credits=%d  debits=%d\n", s.credits.Load(), s.debits.Load())
	if len(lat) > 0 {
		fmt.Printf("latency:         p50=%s  p90=%s  p95=%s  p99=%s  max=%s\n",
			pct(lat, 0.50).Round(time.Microsecond),
			pct(lat, 0.90).Round(time.Microsecond),
			pct(lat, 0.95).Round(time.Microsecond),
			pct(lat, 0.99).Round(time.Microsecond),
			lat[len(lat)-1].Round(time.Microsecond))
	}
	fmt.Println("─────────────────────────")
}

func pct(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(q*float64(len(sorted)-1) + 0.5)
	idx = max(idx, 0)
	idx = min(idx, len(sorted)-1)
	return sorted[idx]
}

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func dsnFromEnv() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		getenv("DB_USER", "finance"),
		getenv("DB_PASSWORD", "finance"),
		getenv("DB_HOST", "db"),
		getenv("DB_PORT", "5432"),
		getenv("DB_NAME", "finance"),
	)
}

func main() {
	addr := ":" + getenv("APP_PORT", "8080")

	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		if err := runHealthcheck(addr); err != nil {
			log.Printf("healthcheck failed: %v", err)
			os.Exit(1)
		}
		return
	}

	store, err := NewPgStore(dsnFromEnv())
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	defer store.Close()

	if err := waitForDB(store, 30, time.Second); err != nil {
		log.Fatalf("database not ready: %v", err)
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      newRouter(&api{store: store}),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("finance app listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown error: %v", err)
	}
}

func waitForDB(store Store, attempts int, wait time.Duration) error {
	var err error
	for i := 1; i <= attempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = store.Ping(ctx)
		cancel()
		if err == nil {
			return nil
		}
		log.Printf("waiting for db (%d/%d): %v", i, attempts, err)
		time.Sleep(wait)
	}
	return err
}

func runHealthcheck(addr string) error {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

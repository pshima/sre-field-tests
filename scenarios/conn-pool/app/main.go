// pool-svc is the system under test for the conn-pool scenario. It reproduces a
// database connection-pool exhaustion outage in the style of the well-known
// Postgres "too many clients" / pool-timeout incidents.
//
// The service holds a small pool of database connections. Each /order request
// runs a slow query (SELECT pg_sleep) that holds its connection for several
// seconds — a stand-in for a genuinely slow query (e.g. a missing index). Under
// load beyond the pool size, every connection is checked out, so new requests —
// including the health check — cannot acquire a connection and time out. The
// database itself is fine: pg_sleep burns no CPU, so Postgres sits near idle.
// That is the diagnostic trap — it looks like the DB is overloaded, but it is
// the app's pool that is exhausted by slow queries.
//
// The fix baked into the environment: SLOW_QUERY_DISABLED=1 makes /order run a
// fast query, so connections cycle quickly and the pool recovers. Merely
// enlarging the pool (POOL_SIZE) masks the problem rather than fixing it.
//
//	MODE=serve (default) — the HTTP service backed by the pool.
//	MODE=load            — sends /order traffic beyond the pool size.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	switch getenv("MODE", "serve") {
	case "load":
		runLoad()
	default:
		runServe()
	}
}

// ---- serve role -------------------------------------------------------------

func runServe() {
	addr := ":" + getenv("PORT", "8080")
	dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@postgres:5432/postgres?sslmode=disable")
	poolSize := getenvInt("POOL_SIZE", 5)
	slowSeconds := getenvInt("SLOW_SECONDS", 5)
	slowDisabled := getenv("SLOW_QUERY_DISABLED", "") == "1"

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("parse dsn: %v", err)
	}
	cfg.MaxConns = int32(poolSize)

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	defer pool.Close()

	// Wait for the database to accept connections before serving.
	waitForDB(pool)

	mux := http.NewServeMux()

	// /healthz goes through the pool, so when the pool is exhausted by slow
	// /order queries the probe cannot acquire a connection and times out — the
	// outage signal. The acquire is bounded so goroutines don't pile up forever.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		conn, err := pool.Acquire(ctx)
		if err != nil {
			log.Printf("healthz: could not acquire connection from pool: %v", err)
			http.Error(w, "pool exhausted: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer conn.Release()
		var one int
		_ = conn.QueryRow(ctx, "SELECT 1").Scan(&one)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})

	// /order runs the (slow) business query, holding a pool connection.
	mux.HandleFunc("/order", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		conn, err := pool.Acquire(ctx)
		if err != nil {
			http.Error(w, "pool exhausted: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer conn.Release()
		if slowDisabled {
			var one int
			_ = conn.QueryRow(ctx, "SELECT 1").Scan(&one)
			fmt.Fprintf(w, `{"ok":true,"query":"fast"}`+"\n")
			return
		}
		// The slow query: holds the connection for SLOW_SECONDS.
		_, err = conn.Exec(ctx, "SELECT pg_sleep($1)", slowSeconds)
		if err != nil {
			http.Error(w, "query failed: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintf(w, `{"ok":true,"query":"slow","held_seconds":%d}`+"\n", slowSeconds)
	})

	// /debug exposes pool saturation for an investigating SRE.
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		s := pool.Stat()
		fmt.Fprintf(w, "slow_query_enabled=%t pool_max=%d acquired=%d idle=%d total=%d acquire_waits=%d\n",
			!slowDisabled, s.MaxConns(), s.AcquiredConns(), s.IdleConns(), s.TotalConns(), s.EmptyAcquireCount())
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		state := "ENABLED (slow queries hold connections)"
		if slowDisabled {
			state = "disabled (fast queries)"
		}
		log.Printf("pool-svc serving on %s, slow query %s, pool_max=%d", addr, state, poolSize)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}

func waitForDB(pool *pgxpool.Pool) {
	for i := 0; i < 60; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := pool.Ping(ctx)
		cancel()
		if err == nil {
			log.Printf("database is ready")
			return
		}
		log.Printf("waiting for database (%d): %v", i, err)
		time.Sleep(time.Second)
	}
	log.Printf("WARNING: database never became ready; serving anyway")
}

// ---- load role --------------------------------------------------------------

func runLoad() {
	target := getenv("TARGET_URL", "http://pool-svc:8080/order")
	concurrency := getenvInt("CONCURRENCY", 12)
	log.Printf("load: %d workers hammering %s", concurrency, target)
	client := &http.Client{Timeout: 60 * time.Second}
	for i := 0; i < concurrency; i++ {
		go func() {
			for {
				resp, err := client.Get(target)
				if err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
			}
		}()
	}
	select {}
}

// ---- helpers ----------------------------------------------------------------

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

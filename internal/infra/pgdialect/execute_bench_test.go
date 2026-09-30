package pgdialect_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
)

type queryWorkload struct {
	name string
	sql  string
	rows int
}

type querySample struct {
	rows  int
	bytes int64
}

func BenchmarkQueryWorkloads(b *testing.B) {
	seedRows := 100_000
	if value := os.Getenv("PORTCULLIS_BENCH_ROWS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 10_000 || parsed > 1_000_000 {
			b.Fatal("PORTCULLIS_BENCH_ROWS must be between 10000 and 1000000")
		}
		seedRows = parsed
	}
	pool := dbtest.FreshPostgres(b)
	ctx := context.Background()
	seedSQL := fmt.Sprintf(`
CREATE TABLE bench_customers (id integer PRIMARY KEY, name text NOT NULL);
INSERT INTO bench_customers SELECT n, 'customer-' || n FROM generate_series(1, 1000) n;
CREATE TABLE bench_orders (
  id bigint PRIMARY KEY, unindexed_key bigint NOT NULL, customer_id integer NOT NULL,
  category integer NOT NULL, amount bigint NOT NULL, payload text NOT NULL, detail jsonb NOT NULL
);
INSERT INTO bench_orders
SELECT n, n, ((n - 1) %% 1000) + 1, n %% 100, (n * 13) %% 10000,
       repeat('x', 96), jsonb_build_object('id', n, 'status', 'ready')
FROM generate_series(1, %d) n;
CREATE INDEX bench_orders_customer ON bench_orders(customer_id, id);
ANALYZE bench_customers;
ANALYZE bench_orders;`, seedRows)
	if _, err := pool.Exec(ctx, seedSQL); err != nil {
		b.Fatal(err)
	}
	cc := pool.Config().ConnConfig
	target, err := connection.NewTarget(cc.Host, int(cc.Port), cc.Database)
	if err != nil {
		b.Fatal(err)
	}
	cred, err := connection.NewCredential(cc.User, cc.Password)
	if err != nil {
		b.Fatal(err)
	}
	dialect := pgdialect.New(pgdialect.Options{})
	cursor := seedRows * 9 / 10
	joinRows := seedRows / 1000
	if seedRows%1000 >= 42 {
		joinRows++
	}
	workloads := []queryWorkload{
		{"indexed_point", "SELECT id, amount FROM bench_orders WHERE id = 5000", 1},
		{"unindexed_point", "SELECT id, amount FROM bench_orders WHERE unindexed_key = 5000", 1},
		{"indexed_page", "SELECT id, amount FROM bench_orders ORDER BY id LIMIT 100", 100},
		{"deep_offset", fmt.Sprintf("SELECT id, amount FROM bench_orders ORDER BY id LIMIT 100 OFFSET %d", cursor), 100},
		{"keyset_page", fmt.Sprintf("SELECT id, amount FROM bench_orders WHERE id > %d ORDER BY id LIMIT 100", cursor), 100},
		{"aggregate", "SELECT category, count(*), sum(amount) FROM bench_orders GROUP BY category", 100},
		{"unindexed_sort", "SELECT id, amount FROM bench_orders ORDER BY amount * id DESC, id DESC LIMIT 100", 100},
		{"indexed_join", "SELECT o.id, c.name, o.amount FROM bench_orders o JOIN bench_customers c ON c.id = o.customer_id WHERE c.id = 42 ORDER BY o.id", joinRows},
		{"narrow_10k", "SELECT id, amount FROM bench_orders ORDER BY id LIMIT 10000", 10_000},
		{"json_10k", "SELECT id, detail FROM bench_orders ORDER BY id LIMIT 10000", 10_000},
		{"wide_2000", "SELECT id, repeat(payload, 128) FROM bench_orders ORDER BY id LIMIT 2000", 2000},
		{"single_large_cell", "SELECT repeat(payload, 300000) FROM bench_orders WHERE id = 1", 1},
	}
	for _, workload := range workloads {
		b.Run(workload.name, func(b *testing.B) {
			statement, err := dialect.ParseSingle(workload.sql)
			if err != nil {
				b.Fatal(err)
			}
			class, err := dialect.Classify(statement)
			if err != nil || class != query.ClassRead {
				b.Fatalf("workload must classify as read: class=%s err=%v", class, err)
			}
			var planJSON []byte
			if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+workload.sql).Scan(&planJSON); err != nil {
				b.Fatal(err)
			}
			var plans []struct {
				ExecutionMS float64 `json:"Execution Time"`
			}
			if err := json.Unmarshal(planJSON, &plans); err != nil || len(plans) != 1 {
				b.Fatalf("invalid EXPLAIN result: %v", err)
			}
			for _, path := range []string{"raw_pgconn", "portcullis"} {
				b.Run(path, func(b *testing.B) {
					b.ReportAllocs()
					durations := make([]float64, 0, b.N)
					var sample querySample
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
						start := time.Now()
						if path == "portcullis" {
							sample, err = sampleAdapter(runCtx, dialect, target, cred, workload.sql)
						} else {
							sample, err = sampleRaw(runCtx, cc.Config.Copy(), workload.sql)
						}
						durations = append(durations, float64(time.Since(start).Microseconds())/1000)
						cancel()
						if err != nil {
							b.Fatal(err)
						}
						if sample.rows != workload.rows {
							b.Fatalf("rows=%d, want %d", sample.rows, workload.rows)
						}
					}
					b.StopTimer()
					slices.Sort(durations)
					b.ReportMetric(percentile(durations, .50), "p50_ms")
					b.ReportMetric(percentile(durations, .95), "p95_ms")
					b.ReportMetric(plans[0].ExecutionMS, "db_exec_ms")
					b.ReportMetric(float64(sample.rows), "rows/op")
					b.ReportMetric(float64(sample.bytes), "payload_bytes/op")
				})
			}
		})
	}
}

func sampleRaw(ctx context.Context, cfg *pgconn.Config, sql string) (sample querySample, err error) {
	conn, err := pgconn.ConnectConfig(ctx, cfg)
	if err != nil {
		return sample, err
	}
	defer func() {
		closeErr := conn.Close(context.WithoutCancel(ctx))
		if err == nil {
			err = closeErr
		}
	}()
	if _, err := conn.Exec(ctx, "BEGIN READ ONLY").ReadAll(); err != nil {
		return sample, err
	}
	rr := conn.ExecParams(ctx, sql, nil, nil, nil, nil)
	for rr.NextRow() {
		sample.rows++
		for _, value := range rr.Values() {
			sample.bytes += int64(len(value))
		}
	}
	if _, err := rr.Close(); err != nil {
		return sample, err
	}
	_, err = conn.Exec(ctx, "COMMIT").ReadAll()
	return sample, err
}

func sampleAdapter(ctx context.Context, dialect *pgdialect.Dialect, target connection.Target, cred connection.Credential, sql string) (sample querySample, err error) {
	stream, err := dialect.Execute(ctx, target, connection.TLSModeDisable, cred, query.Execution{SQL: sql, Class: query.ClassRead})
	if err != nil {
		return sample, err
	}
	defer func() {
		closeErr := stream.Close()
		if err == nil {
			err = closeErr
		}
	}()
	for stream.Next() {
		sample.rows++
		for _, cell := range stream.Row() {
			sample.bytes += int64(len(cell.Text) + len(cell.Bytes))
		}
	}
	return sample, stream.Err()
}

func percentile(sorted []float64, fraction float64) float64 {
	return sorted[int(math.Ceil(float64(len(sorted))*fraction))-1]
}

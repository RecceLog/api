package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier is the subset of pgx that repository methods need. Both *pgxpool.Pool
// and pgx.Tx satisfy it, which lets a single repo method run either against
// the pool (no ambient tx) or inside a transaction (when a Transactor.InTx
// is active) without branching.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Transactor opens database transactions and binds them to a context.
type Transactor struct {
	pool *pgxpool.Pool
}

// NewTransactor wires a Transactor to its pool. The pool is also reused as
// the fallback Querier when no transaction is active.
func NewTransactor(pool *pgxpool.Pool) *Transactor {
	return &Transactor{pool: pool}
}

// Pool exposes the underlying pool so repositories can take it once at
// construction time and call Querier(ctx, pool) on each operation.
func (t *Transactor) Pool() *pgxpool.Pool { return t.pool }

type txKey struct{}

// InTx runs fn inside a transaction. Reentrant: if ctx already carries an
// active tx (from an outer InTx), fn runs inline against that same tx — no
// nested BEGIN, no savepoints. This is what lets the HTTP handler open one
// outer tx and have inner services' InTx calls silently join.
//
// The tx commits when fn returns nil, rolls back otherwise. A panic inside
// fn is converted to a rollback + re-panic.
func (t *Transactor) InTx(ctx context.Context, fn func(context.Context) error) (err error) {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// querier returns the tx bound to ctx if InTx is active, otherwise pool.
// Repos call this on every operation so they participate in an ambient tx
// without needing to know whether one exists.
func querier(ctx context.Context, pool *pgxpool.Pool) Querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

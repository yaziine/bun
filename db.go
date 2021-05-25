package bun

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"

	"github.com/uptrace/bun/dialect/feature"
	"github.com/uptrace/bun/internal"
	"github.com/uptrace/bun/schema"
)

const (
	discardUnknownColumns internal.Flag = 1 << iota
)

type DBStats struct {
	Queries uint64
	Errors  uint64
}

type DBOption func(db *DB)

func WithDiscardUnknownColumns() DBOption {
	return func(db *DB) {
		db.flags = db.flags.Set(discardUnknownColumns)
	}
}

type DB struct {
	*sql.DB
	dialect  schema.Dialect
	features feature.Feature

	queryHooks []QueryHook

	fmter schema.Formatter
	flags internal.Flag

	stats DBStats
}

func NewDB(sqldb *sql.DB, dialect schema.Dialect, opts ...DBOption) *DB {
	db := &DB{
		DB:       sqldb,
		dialect:  dialect,
		features: dialect.Features(),
		fmter:    schema.NewFormatter(dialect),
	}

	for _, opt := range opts {
		opt(db)
	}

	return db
}

func (db *DB) Stats() DBStats {
	return DBStats{
		Queries: atomic.LoadUint64(&db.stats.Queries),
		Errors:  atomic.LoadUint64(&db.stats.Errors),
	}
}

func (db *DB) NewValues(model interface{}) *ValuesQuery {
	return NewValuesQuery(db, model)
}

func (db *DB) NewSelect() *SelectQuery {
	return NewSelectQuery(db)
}

func (db *DB) NewInsert() *InsertQuery {
	return NewInsertQuery(db)
}

func (db *DB) NewUpdate() *UpdateQuery {
	return NewUpdateQuery(db)
}

func (db *DB) NewDelete() *DeleteQuery {
	return NewDeleteQuery(db)
}

func (db *DB) NewCreateTable() *CreateTableQuery {
	return NewCreateTableQuery(db)
}

func (db *DB) NewDropTable() *DropTableQuery {
	return NewDropTableQuery(db)
}

func (db *DB) NewCreateIndex() *CreateIndexQuery {
	return NewCreateIndexQuery(db)
}

func (db *DB) NewDropIndex() *DropIndexQuery {
	return NewDropIndexQuery(db)
}

func (db *DB) NewTruncateTable() *TruncateTableQuery {
	return NewTruncateTableQuery(db)
}

func (db *DB) NewAddColumn() *AddColumnQuery {
	return NewAddColumnQuery(db)
}

func (db *DB) NewDropColumn() *DropColumnQuery {
	return NewDropColumnQuery(db)
}

func (db *DB) ResetModel(ctx context.Context, models ...interface{}) error {
	for _, model := range models {
		if _, err := db.NewDropTable().Model(model).IfExists().Exec(ctx); err != nil {
			return err
		}
		if _, err := db.NewCreateTable().Model(model).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) Dialect() schema.Dialect {
	return db.dialect
}

func (db *DB) ScanRows(ctx context.Context, rows *sql.Rows, dest ...interface{}) error {
	model, err := newModel(db, dest)
	if err != nil {
		return err
	}

	_, err = model.ScanRows(ctx, rows)
	return err
}

func (db *DB) ScanRow(ctx context.Context, rows *sql.Rows, dest ...interface{}) error {
	model, err := newModel(db, dest)
	if err != nil {
		return err
	}

	rs, ok := model.(rowScanner)
	if !ok {
		return fmt.Errorf("bun: %T does not support ScanRow", model)
	}

	return rs.ScanRow(ctx, rows)
}

func (db *DB) AddQueryHook(hook QueryHook) {
	db.queryHooks = append(db.queryHooks, hook)
}

func (db *DB) Table(typ reflect.Type) *schema.Table {
	return db.dialect.Tables().Get(typ)
}

func (db *DB) RegisterModel(models ...interface{}) {
	db.dialect.Tables().Register(models...)
}

func (db *DB) clone() *DB {
	clone := *db

	l := len(clone.queryHooks)
	clone.queryHooks = clone.queryHooks[:l:l]

	return &clone
}

func (db *DB) WithNamedArg(name string, value interface{}) *DB {
	clone := db.clone()
	clone.fmter = clone.fmter.WithArg(name, value)
	return clone
}

func (db *DB) NamedArg(name string) interface{} {
	return db.fmter.Arg(name)
}

func (db *DB) Formatter() schema.Formatter {
	return db.fmter
}

//------------------------------------------------------------------------------

func (db *DB) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.ExecContext(context.Background(), query, args...)
}

func (db *DB) ExecContext(
	ctx context.Context, query string, args ...interface{},
) (sql.Result, error) {
	ctx, event := db.beforeQuery(ctx, nil, query, args)
	res, err := db.DB.ExecContext(ctx, db.format(query, args))
	db.afterQuery(ctx, event, res, err)

	return res, err
}

func (db *DB) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return db.QueryContext(context.Background(), query, args...)
}

func (db *DB) QueryContext(
	ctx context.Context, query string, args ...interface{},
) (*sql.Rows, error) {
	ctx, event := db.beforeQuery(ctx, nil, query, args)
	rows, err := db.DB.QueryContext(ctx, db.format(query, args))
	db.afterQuery(ctx, event, nil, err)
	return rows, err
}

func (db *DB) QueryRow(query string, args ...interface{}) *sql.Row {
	return db.QueryRowContext(context.Background(), query, args...)
}

func (db *DB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	ctx, event := db.beforeQuery(ctx, nil, query, args)
	row := db.DB.QueryRowContext(ctx, db.format(query, args))
	db.afterQuery(ctx, event, nil, row.Err())
	return row
}

func (db *DB) format(query string, args []interface{}) string {
	return db.fmter.FormatQuery(query, args...)
}

//------------------------------------------------------------------------------

type Conn struct {
	db *DB
	*sql.Conn
}

func (db *DB) Conn(ctx context.Context) (Conn, error) {
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		return Conn{}, err
	}
	return Conn{
		db:   db,
		Conn: conn,
	}, nil
}

func (c Conn) ExecContext(
	ctx context.Context, query string, args ...interface{},
) (sql.Result, error) {
	ctx, event := c.db.beforeQuery(ctx, nil, query, args)
	res, err := c.Conn.ExecContext(ctx, c.db.format(query, args))
	c.db.afterQuery(ctx, event, res, err)
	return res, err
}

func (c Conn) QueryContext(
	ctx context.Context, query string, args ...interface{},
) (*sql.Rows, error) {
	ctx, event := c.db.beforeQuery(ctx, nil, query, args)
	rows, err := c.Conn.QueryContext(ctx, c.db.format(query, args))
	c.db.afterQuery(ctx, event, nil, err)
	return rows, err
}

func (c Conn) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	ctx, event := c.db.beforeQuery(ctx, nil, query, args)
	row := c.Conn.QueryRowContext(ctx, c.db.format(query, args))
	c.db.afterQuery(ctx, event, nil, row.Err())
	return row
}

func (c Conn) NewValues(model interface{}) *ValuesQuery {
	return NewValuesQuery(c.db, model).DB(c)
}

func (c Conn) NewSelect() *SelectQuery {
	return NewSelectQuery(c.db).DB(c)
}

func (c Conn) NewInsert() *InsertQuery {
	return NewInsertQuery(c.db).DB(c)
}

func (c Conn) NewUpdate() *UpdateQuery {
	return NewUpdateQuery(c.db).DB(c)
}

func (c Conn) NewDelete() *DeleteQuery {
	return NewDeleteQuery(c.db).DB(c)
}

func (c Conn) NewCreateTable() *CreateTableQuery {
	return NewCreateTableQuery(c.db).DB(c)
}

func (c Conn) NewDropTable() *DropTableQuery {
	return NewDropTableQuery(c.db).DB(c)
}

func (c Conn) NewCreateIndex() *CreateIndexQuery {
	return NewCreateIndexQuery(c.db).DB(c)
}

func (c Conn) NewDropIndex() *DropIndexQuery {
	return NewDropIndexQuery(c.db).DB(c)
}

func (c Conn) NewTruncateTable() *TruncateTableQuery {
	return NewTruncateTableQuery(c.db).DB(c)
}

func (c Conn) NewAddColumn() *AddColumnQuery {
	return NewAddColumnQuery(c.db).DB(c)
}

func (c Conn) NewDropColumn() *DropColumnQuery {
	return NewDropColumnQuery(c.db).DB(c)
}

//------------------------------------------------------------------------------

type Stmt struct {
	*sql.Stmt
}

func (db *DB) Prepare(query string) (Stmt, error) {
	return db.PrepareContext(context.Background(), query)
}

func (db *DB) PrepareContext(ctx context.Context, query string) (Stmt, error) {
	stmt, err := db.DB.PrepareContext(ctx, query)
	if err != nil {
		return Stmt{}, err
	}
	return Stmt{Stmt: stmt}, nil
}

type Tx struct {
	db *DB
	*sql.Tx
}

func (db *DB) Begin() (Tx, error) {
	return db.BeginTx(context.Background(), nil)
}

func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error) {
	tx, err := db.DB.BeginTx(ctx, opts)
	if err != nil {
		return Tx{}, err
	}
	return Tx{
		db: db,
		Tx: tx,
	}, nil
}

func (tx Tx) Exec(query string, args ...interface{}) (sql.Result, error) {
	return tx.ExecContext(context.TODO(), query, args...)
}

func (tx Tx) ExecContext(
	ctx context.Context, query string, args ...interface{},
) (sql.Result, error) {
	ctx, event := tx.db.beforeQuery(ctx, nil, query, args)
	res, err := tx.Tx.ExecContext(ctx, tx.db.format(query, args))
	tx.db.afterQuery(ctx, event, res, err)
	return res, err
}

func (tx Tx) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return tx.QueryContext(context.TODO(), query, args...)
}

func (tx Tx) QueryContext(
	ctx context.Context, query string, args ...interface{},
) (*sql.Rows, error) {
	ctx, event := tx.db.beforeQuery(ctx, nil, query, args)
	rows, err := tx.Tx.QueryContext(ctx, tx.db.format(query, args))
	tx.db.afterQuery(ctx, event, nil, err)
	return rows, err
}

func (tx Tx) QueryRow(query string, args ...interface{}) *sql.Row {
	return tx.QueryRowContext(context.TODO(), query, args...)
}

func (tx Tx) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	ctx, event := tx.db.beforeQuery(ctx, nil, query, args)
	row := tx.Tx.QueryRowContext(ctx, tx.db.format(query, args))
	tx.db.afterQuery(ctx, event, nil, row.Err())
	return row
}

func (tx Tx) NewValues(model interface{}) *ValuesQuery {
	return NewValuesQuery(tx.db, model).DB(tx)
}

func (tx Tx) NewSelect() *SelectQuery {
	return NewSelectQuery(tx.db).DB(tx)
}

func (tx Tx) NewInsert() *InsertQuery {
	return NewInsertQuery(tx.db).DB(tx)
}

func (tx Tx) NewUpdate() *UpdateQuery {
	return NewUpdateQuery(tx.db).DB(tx)
}

func (tx Tx) NewDelete() *DeleteQuery {
	return NewDeleteQuery(tx.db).DB(tx)
}

func (tx Tx) NewCreateTable() *CreateTableQuery {
	return NewCreateTableQuery(tx.db).DB(tx)
}

func (tx Tx) NewDropTable() *DropTableQuery {
	return NewDropTableQuery(tx.db).DB(tx)
}

func (tx Tx) NewCreateIndex() *CreateIndexQuery {
	return NewCreateIndexQuery(tx.db).DB(tx)
}

func (tx Tx) NewDropIndex() *DropIndexQuery {
	return NewDropIndexQuery(tx.db).DB(tx)
}

func (tx Tx) NewTruncateTable() *TruncateTableQuery {
	return NewTruncateTableQuery(tx.db).DB(tx)
}

func (tx Tx) NewAddColumn() *AddColumnQuery {
	return NewAddColumnQuery(tx.db).DB(tx)
}

func (tx Tx) NewDropColumn() *DropColumnQuery {
	return NewDropColumnQuery(tx.db).DB(tx)
}

//------------------------------------------------------------------------------

type result struct {
	r sql.Result
	n int
}

func (r result) RowsAffected() (int64, error) {
	if r.r != nil {
		return r.r.RowsAffected()
	}
	return int64(r.n), nil
}

func (r result) LastInsertId() (int64, error) {
	if r.r != nil {
		return r.r.LastInsertId()
	}
	return 0, errors.New("LastInsertId is not available")
}

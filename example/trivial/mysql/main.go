package main

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/uptrace/bun"
	"github.com/yaziine/bun/dialect/mysqldialect"
	"github.com/yaziine/bun/extra/bundebug"
)

func main() {
	ctx := context.Background()

	// Open a MySQL 5.7+ database.
	sqldb, err := sql.Open("mysql", "root:pass@/test")
	if err != nil {
		panic(err)
	}

	// Create a Bun db on top of it.
	db := bun.NewDB(sqldb, mysqldialect.New())

	// Print all queries to stdout.
	db.AddQueryHook(bundebug.NewQueryHook(bundebug.WithVerbose(true)))

	var rnd float64

	// Select a random number.
	if err := db.NewSelect().ColumnExpr("rand()").Scan(ctx, &rnd); err != nil {
		panic(err)
	}

	fmt.Println(rnd)
}

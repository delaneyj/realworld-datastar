package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/delaneyj/realworld-datastar/sql"
	"github.com/delaneyj/realworld-datastar/web"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {

	db, err := sql.SetupDB(ctx, "data", false)
	if err != nil {
		return fmt.Errorf("failed to setup database: %w", err)
	}

	defer db.Close()

	return web.RunHTTPServer(ctx, db)
}

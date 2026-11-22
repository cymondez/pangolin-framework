package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	broker "github.com/cymondez/pangolin-framework/events/nats"
)

func run() error {
	provision := flag.Bool("provision", false, "create the demo stream and exit")
	flag.Parse()
	if !*provision {
		return fmt.Errorf("use -provision to create this example's stream")
	}
	url := os.Getenv("PANGOLIN_NATS_URL")
	if url == "" {
		return fmt.Errorf("PANGOLIN_NATS_URL is required")
	}
	client, err := broker.Connect(url, "BOOKSTORE_DEMO", "bookstore.demo.book_created")
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Provision(ctx); err != nil {
		return err
	}
	fmt.Println("Bookstore event stream is ready.")
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

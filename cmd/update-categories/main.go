package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mrmohebi/divar-alert/divar"
)

func main() {
	output := flag.String("output", divar.DefaultCategoriesFile, "Path to the category JSON file")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	catalog, err := divar.UpdateCategories(ctx, *output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Saved %d categories to %s (generated %s)\n", catalog.TotalCategories, *output, catalog.GeneratedAt.Format("2006-01-02T15:04:05Z"))
}

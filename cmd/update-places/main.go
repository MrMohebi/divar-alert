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
	output := flag.String("output", divar.DefaultPlacesFile, "Path to the places JSON file")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	catalog, err := divar.UpdatePlaces(ctx, *output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Saved %d cities in %d provinces to %s (generated %s)\n", catalog.TotalCities, catalog.TotalProvinces, *output, catalog.GeneratedAt.Format("2006-01-02T15:04:05Z"))
}

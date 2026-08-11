package main

import (
	"flag"
	"fmt"
	"os"

	"smart-bill-manager/internal/devtools/regressionfixtures"
)

func main() {
	outputDir := flag.String("out", "internal/services/testdata/regression", "fixture output directory")
	flag.Parse()
	if err := regressionfixtures.Generate(*outputDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

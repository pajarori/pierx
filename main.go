package main

import (
	"os"

	"github.com/pajarori/pierx/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

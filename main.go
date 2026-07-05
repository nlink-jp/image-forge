package main

import (
	"fmt"
	"os"

	"github.com/nlink-jp/image-forge/internal/cli"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := cli.Run(version, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "image-forge:", err)
		os.Exit(1)
	}
}

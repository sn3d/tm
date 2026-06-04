package main

import (
	"context"
	"log"
	"os"

	"github.com/sn3d/tm/cmd"
)

var version = "dev"

func main() {
	cmd.Root.Version = version
	if err := cmd.Root.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

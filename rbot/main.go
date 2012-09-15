package main

import (
	"log"
	"os"
	"time"
	"github.com/wiccatech/rbot"
)

func main() {
	logger := log.New(os.Stderr, "rbot ", log.LstdFlags)

	if len(os.Args) < 3 {
		logger.Printf("usage: %s <mongodb-server> <db-name>", os.Args[0])
		os.Exit(1)
	}

	for {
		logger.Printf("launching bot\n")
		err := rbot.Run(logger, os.Args[1], os.Args[2])
		logger.Printf("bot failed: %s\n", err)
		time.Sleep(300 * time.Second)
	}
}

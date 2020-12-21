package main

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"
)

var logger *zap.SugaredLogger

func main() {
	config, err := parseConfig()
	if err != nil {
		fmt.Printf("parseConfig error: %s\n", err)
		os.Exit(-1)
	}

	logger = NewLogger(config)
	defer logger.Sync()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sl, err := NewAWSServerless(config)
	if err != nil {
		logger.Fatalf("NewAWSServerless, %s\n", err)
	}

	if err := sl.Invoke(ctx); err != nil {
		logger.Fatalf("Invoke error, %s\n", err)
	}
}

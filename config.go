package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Config struct
type Config struct {
	funcName string
	vendor   Vendor
	json     bool

	payload string // request payload
}

// Vendor describe vendor string
type Vendor string

const (
	// VendorAWS is a AWS vendor name
	VendorAWS Vendor = "aws"
	// VendorGCP is a GCP vendor name
	VendorGCP Vendor = "gcp"
)

func parseConfig() (*Config, error) {
	var funcName string
	var vendor string
	var json bool
	var payload string
	var payloadFile string

	flag.StringVar(&funcName, "func", "", "function name")
	flag.StringVar(&vendor, "vendor", "aws", `vendor name(currently only "aws")`)
	flag.BoolVar(&json, "json", false, "enable JSON log format")
	flag.StringVar(&payload, "payload", "", "request payload. higher priority than file")
	flag.StringVar(&payloadFile, "payload_file", "", "speficy request payload file")
	// convert Environment Variables to flags
	flag.VisitAll(func(f *flag.Flag) {
		if s := os.Getenv(strings.ToUpper(f.Name)); s != "" {
			f.Value.Set(s)
		}
	})

	flag.Parse()

	if funcName == "" {
		return nil, fmt.Errorf("func required")
	}

	config := &Config{
		funcName: funcName,
		vendor:   Vendor(strings.ToLower(vendor)),
		json:     json,
	}

	// read payload file if payload is not specified
	if payloadFile != "" && payload == "" {
		buf, err := ioutil.ReadFile(payloadFile)
		if err != nil {
			return nil, fmt.Errorf("read payload file, %s: %w", payloadFile, err)
		}
		config.payload = string(buf)
	}
	if payload != "" {
		config.payload = payload
	}

	return config, nil
}

func NewLogger(config *Config) *zap.SugaredLogger {
	level := zap.NewAtomicLevel()
	level.SetLevel(zapcore.InfoLevel)

	zapConfig := zap.Config{
		Level: level,
		EncoderConfig: zapcore.EncoderConfig{
			MessageKey:  "msg",
			TimeKey:     "time",
			EncodeTime:  zapcore.ISO8601TimeEncoder,
			LevelKey:    "level",
			EncodeLevel: zapcore.CapitalLevelEncoder,
			// caller is disabled currently
			//                      CallerKey:    "caller",
			//                      EncodeCaller: zapcore.ShortCallerEncoder,
		},
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stdout"},
	}
	if config.json {
		zapConfig.Encoding = "json"
	} else {
		zapConfig.Encoding = "console"
	}

	l, err := zapConfig.Build()
	if err != nil {
		panic(err)
	}
	return l.Sugar()
}

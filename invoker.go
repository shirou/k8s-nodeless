package main

import "context"

// Invoker is an interface for serverless functions
type Invoker interface {
	Invoke(ctx context.Context)
}

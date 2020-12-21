package main

import (
	"fmt"
	"testing"
)

func TestAWSGetRequestId(t *testing.T) {
	s := startRequestRe.FindStringSubmatch("END RequestId: 2e3c63b7-0681-4e60-9767-b025b0714db1")
	for _, ss := range s {
		fmt.Println(ss)
	}
}

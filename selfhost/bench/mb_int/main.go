package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

func main() {
	n, _ := strconv.Atoi(os.Args[1])
	acc := 0
	for i := 0; i < n; i++ {
		acc = (acc + (i*31 + 7)) % 1000003
	}
	t0 := time.Now()
	acc = 0
	for i := 0; i < n; i++ {
		acc = (acc + (i*31 + 7)) % 1000003
	}
	fmt.Printf("int acc=%d ms=%d\n", acc, time.Since(t0).Milliseconds())
}

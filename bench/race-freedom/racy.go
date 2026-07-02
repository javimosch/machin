// The same textbook race in Go: go build compiles this silently — no error,
// no warning. Only `go run -race` (opt-in, dynamic) catches it, and only for
// interleavings the specific run happens to hit.
package main

import "fmt"

var counter = 0

func incr() {
	for i := 0; i < 2000000; i++ {
		counter = counter + 1
	}
}

func main() {
	done := make(chan bool, 4)
	for g := 0; g < 4; g++ {
		go func() {
			incr()
			done <- true
		}()
	}
	for g := 0; g < 4; g++ {
		<-done
	}
	fmt.Println(counter)
}

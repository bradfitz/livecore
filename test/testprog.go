package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

func main() {
	fmt.Printf("Test program PID: %d\n", os.Getpid())

	// Allocate some memory
	mem := make([]byte, 1024*1024) // 1MB
	for i := range mem {
		mem[i] = byte(i % 256)
	}

	// Start some goroutines
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			counter := 0
			for {
				counter++
				if counter%1000000 == 0 {
					fmt.Printf("Goroutine %d: counter = %d\n", id, counter)
				}
				time.Sleep(100 * time.Millisecond)
			}
		}(i)
	}

	// Keep the program running
	fmt.Println("Test program running... Press Ctrl+C to exit")
	select {}
}

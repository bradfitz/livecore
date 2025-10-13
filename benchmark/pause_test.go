package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

var counter int64
var maxPause int64

func main() {
	fmt.Printf("Pause test program PID: %d\n", os.Getpid())

	// Start a goroutine that continuously increments a counter
	go func() {
		for {
			start := time.Now()
			atomic.AddInt64(&counter, 1)
			elapsed := time.Since(start)

			// Track maximum pause
			if elapsed.Nanoseconds() > atomic.LoadInt64(&maxPause) {
				atomic.StoreInt64(&maxPause, elapsed.Nanoseconds())
			}

			// Print progress every million increments
			if atomic.LoadInt64(&counter)%1000000 == 0 {
				fmt.Printf("Counter: %d, Max pause: %v\n",
					atomic.LoadInt64(&counter),
					time.Duration(atomic.LoadInt64(&maxPause)))
			}
		}
	}()

	// Start multiple goroutines to create contention
	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				atomic.AddInt64(&counter, 1)
				runtime.Gosched()
			}
		}()
	}

	// Keep the program running
	fmt.Println("Pause test program running... Press Ctrl+C to exit")
	select {}
}

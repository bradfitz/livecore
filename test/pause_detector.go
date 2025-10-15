package main

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

var (
	lastIncrement int64
	maxPause      int64
	counter       int64
)

func main() {
	fmt.Printf("Pause Detector Test Program\n")
	fmt.Printf("PID: %d\n", os.Getpid())
	fmt.Println()
	fmt.Println("This program detects pauses in execution by monitoring")
	fmt.Println("the time between counter increments.")
	fmt.Println()

	// Start a goroutine that continuously increments
	go func() {
		for {
			now := time.Now().UnixNano()
			atomic.StoreInt64(&lastIncrement, now)
			atomic.AddInt64(&counter, 1)
			time.Sleep(100 * time.Microsecond)
		}
	}()

	// Monitor for pauses
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			now := time.Now().UnixNano()
			lastInc := atomic.LoadInt64(&lastIncrement)

			gap := now - lastInc
			currentMax := atomic.LoadInt64(&maxPause)

			if gap > currentMax {
				atomic.StoreInt64(&maxPause, gap)
			}

			// Print if we detect a significant pause
			if gap > 100*1000*1000 { // 100ms
				duration := time.Duration(gap)
				fmt.Printf("[%s] PAUSE DETECTED: %v\n",
					time.Now().Format("15:04:05.000"), duration)
			}
		}
	}()

	// Periodic status reporting
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			maxPauseDuration := time.Duration(atomic.LoadInt64(&maxPause))
			counterVal := atomic.LoadInt64(&counter)
			fmt.Printf("[Status] Counter: %d, Max pause: %v\n",
				counterVal, maxPauseDuration)
		}
	}()

	fmt.Println("Ready for core dump testing.")
	fmt.Println("Run: livecore", os.Getpid(), "output.core")
	fmt.Println()
	fmt.Println("Press Ctrl+C to exit")

	// Keep running
	select {}
}

package signals

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var cleanupLock sync.Mutex
var cleanupFn []func()

func HandleStopSignals() {
	s := make(chan os.Signal, 10)
	signal.Notify(s, os.Interrupt, syscall.SIGTERM)
	go func() {
		for range s {
			fmt.Println("Cleanup")
			cleanupLock.Lock()
			for _, f := range cleanupFn {
				f()
			}
			os.Exit(1)
		}
	}()
}

func AddCleanupFn(fn func()) {
	cleanupLock.Lock()
	defer cleanupLock.Unlock()
	cleanupFn = append(cleanupFn, fn)
}

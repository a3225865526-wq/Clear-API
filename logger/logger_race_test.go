package logger

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// Regression guard for the data race on logCount/setupLogWorking: concurrent
// loggers used to do unsynchronized read-modify-write on package globals.
// Run with -race; any reintroduction of the race fails this test.
func TestLogHelperConcurrentLoggersDoNotRace(t *testing.T) {
	oldWriter, oldErrWriter := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = io.Discard, io.Discard
	t.Cleanup(func() {
		gin.DefaultWriter, gin.DefaultErrorWriter = oldWriter, oldErrWriter
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				LogInfo(context.Background(), "race-guard info")
				LogError(context.Background(), "race-guard error")
			}
		}(i)
	}
	wg.Wait()

	if logCount.Load() < 8*500*2 {
		t.Fatalf("log count not observed atomically: %d", logCount.Load())
	}
	if setupLogWorking.Load() {
		t.Fatal("rotation flag left engaged without SetupLogger running")
	}
}

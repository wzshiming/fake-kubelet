package fake_kubelet

import (
	"testing"
	"time"
)

func TestParallelTasks(t *testing.T) {
	tasks := newParallelTasks(4)
	startTime := time.Now()
	for i := 0; i < 10; i++ {
		tasks.Add(func() {
			time.Sleep(1 * time.Second)
		})
	}

	tasks.Wait()
	elapsed := time.Since(startTime)
	if elapsed >= 4*time.Second {
		t.Fatalf("Tasks took too long to complete: %v", elapsed)
	} else if elapsed < 3*time.Second {
		t.Fatalf("Tasks completed too quickly: %v", elapsed)
	}
	t.Logf("Tasks completed in %v", elapsed)
}

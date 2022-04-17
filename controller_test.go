package fake_kubelet

import (
	"testing"
)

type testingLogger struct {
	*testing.T
}

func (t testingLogger) Printf(format string, args ...interface{}) {
	t.Logf(format, args...)
}

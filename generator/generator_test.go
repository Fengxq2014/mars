package generator

import (
	"testing"
	"time"
)

func TestGenerator_GetTime(t *testing.T) {
	generator := New(1)
	// Pin the configured epoch independently of the host's timezone.
	expect := time.UnixMilli(1830272941280).Local().Format(time.RFC3339Nano)
	if actual := generator.GetTime(530703474355077120); actual != expect {
		t.Fatalf("GetTime: got %s, want %s", actual, expect)
	}
}

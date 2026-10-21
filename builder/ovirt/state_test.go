package ovirt

import (
	"strings"
	"testing"
	"time"
)

// fastPoll makes WaitForState poll without sleeping for the duration of a test.
func fastPoll(t *testing.T) {
	t.Helper()
	old := pollInterval
	pollInterval = time.Millisecond
	t.Cleanup(func() { pollInterval = old })
}

// refreshSequence returns the given states in order, then repeats the last one.
func refreshSequence(states ...string) StateRefreshFunc {
	n := 0
	return func() (any, string, error) {
		s := states[min(n, len(states)-1)]
		n++
		return s, s, nil
	}
}

func TestWaitForState_NotFoundIsRetried(t *testing.T) {
	fastPoll(t)
	_, err := WaitForState(&StateChangeConf{
		Pending: []string{"image_locked"},
		Target:  []string{"down"},
		Refresh: refreshSequence("", "", "image_locked", "down"),
	})
	if err != nil {
		t.Fatalf("expected the wait to succeed, got %s", err)
	}
}

func TestWaitForState_NotFoundGivesUp(t *testing.T) {
	fastPoll(t)
	_, err := WaitForState(&StateChangeConf{
		Pending: []string{"image_locked"},
		Target:  []string{"down"},
		Refresh: refreshSequence(""),
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected a not found error, got %v", err)
	}
}

func TestWaitForState_Timeout(t *testing.T) {
	fastPoll(t)
	_, err := WaitForState(&StateChangeConf{
		Pending: []string{"powering_down"},
		Target:  []string{"down"},
		Refresh: refreshSequence("powering_down"),
		Timeout: 50 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected a timeout error, got %v", err)
	}
}

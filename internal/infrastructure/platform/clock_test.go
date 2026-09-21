package platform

import (
	"testing"
	"time"
)

func TestSystemClockNowUsesSystemTime(t *testing.T) {
	before := time.Now()
	got := SystemClock{}.Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Errorf("Now() = %v, esperado entre %v e %v", got, before, after)
	}
}

func TestFixedClockNow(t *testing.T) {
	at := time.Date(2026, 9, 21, 10, 30, 0, 0, time.UTC)
	clock := NewFixedClock(at)

	if got := clock.Now(); !got.Equal(at) {
		t.Errorf("Now() = %v, esperado %v", got, at)
	}
	if got := clock.Now(); !got.Equal(at) {
		t.Errorf("segunda chamada = %v, esperado o mesmo instante %v", got, at)
	}
}

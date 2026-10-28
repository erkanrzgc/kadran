package totp

import (
	"testing"
	"time"
)

func TestLockDuration(t *testing.T) {
	for _, tc := range []struct {
		failures int
		want     time.Duration
	}{
		{0, 0}, {1, 0}, {4, 0},
		{5, time.Minute}, {6, 2 * time.Minute}, {7, 4 * time.Minute},
		{11, time.Hour}, {50, time.Hour},
	} {
		if got := LockDuration(tc.failures); got != tc.want {
			t.Errorf("%d başarısızlık: %v, beklenen %v", tc.failures, got, tc.want)
		}
	}
}

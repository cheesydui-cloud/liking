package agent

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

func TestNTPTransmitTime(t *testing.T) {
	pkt := make([]byte, 48)
	// 2026-11-20 06:17:13 UTC
	want := time.Date(2026, 11, 20, 6, 17, 13, 0, time.UTC)
	sec := uint32(want.Unix() + 2208988800)
	binary.BigEndian.PutUint32(pkt[40:44], sec)
	got, err := ntpTransmitTime(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
	if _, err := ntpTransmitTime(pkt[:40]); err == nil {
		t.Fatal("short packet should fail")
	}
}

func TestAlignClockStepsWhenSlow(t *testing.T) {
	remote := time.Date(2026, 11, 20, 6, 17, 13, 0, time.UTC)
	now := remote.Add(-33 * time.Second)
	restore := stubClock(&now, remote, true)
	defer restore()

	var setTo time.Time
	setSystemClock = func(ts time.Time) error {
		setTo = ts
		now = remote
		return nil
	}
	rep := AlignClock(context.Background(), false)
	if rep.Err != nil {
		t.Fatal(rep.Err)
	}
	if !rep.Stepped || !setTo.Equal(remote) {
		t.Fatalf("stepped=%v set=%s", rep.Stepped, setTo)
	}
	if rep.Skew != -33*time.Second {
		t.Fatalf("skew %s", rep.Skew)
	}
	if rep.After != 0 {
		t.Fatalf("after %s", rep.After)
	}
	if !rep.NTP {
		t.Fatal("ntp should stay on")
	}
}

func TestAlignClockLeavesSmallSkew(t *testing.T) {
	remote := time.Date(2026, 11, 20, 6, 17, 13, 0, time.UTC)
	now := remote.Add(-200 * time.Millisecond)
	restore := stubClock(&now, remote, true)
	defer restore()
	setSystemClock = func(time.Time) error {
		t.Fatal("should not step")
		return nil
	}
	rep := AlignClock(context.Background(), false)
	if rep.Err != nil || rep.Stepped {
		t.Fatalf("%+v", rep)
	}
}

func TestAlignClockInstallsWhenNTPMissing(t *testing.T) {
	remote := time.Date(2026, 11, 20, 6, 17, 13, 0, time.UTC)
	now := remote
	restore := stubClock(&now, remote, false)
	defer restore()
	installed := 0
	installTimeSync = func() error {
		installed++
		return nil
	}
	enableNTP = func() error { return nil }
	rep := AlignClock(context.Background(), false)
	if rep.Err != nil {
		t.Fatal(rep.Err)
	}
	if installed != 1 {
		t.Fatalf("installs %d", installed)
	}
	rep = AlignClock(context.Background(), false)
	if installed != 1 {
		t.Fatalf("second align installed again: %d", installed)
	}
}

func TestAlignClockDoesNotStepWithoutSource(t *testing.T) {
	now := time.Now()
	restore := stubClock(&now, time.Time{}, true)
	defer restore()
	queryClock = func(context.Context) (time.Time, error) {
		return time.Time{}, errors.New("timeout")
	}
	setSystemClock = func(time.Time) error {
		t.Fatal("should not step")
		return nil
	}
	rep := AlignClock(context.Background(), true)
	if rep.Err == nil || rep.Measured {
		t.Fatalf("%+v", rep)
	}
}

func stubClock(now *time.Time, remote time.Time, ntp bool) func() {
	oldNow, oldQuery, oldSet, oldNTP, oldEnable, oldInstall := clockNow, queryClock, setSystemClock, ntpSynchronized, enableNTP, installTimeSync
	oldInstalled, oldTry, oldOS := timeSyncDidInstall, lastTimeInstall, clockGOOS
	clockNow = func() time.Time { return *now }
	queryClock = func(context.Context) (time.Time, error) { return remote, nil }
	setSystemClock = func(time.Time) error { return nil }
	ntpSynchronized = func() bool { return ntp }
	enableNTP = func() error { return nil }
	installTimeSync = func() error { return nil }
	timeSyncDidInstall = false
	lastTimeInstall = time.Time{}
	clockGOOS = "linux"
	return func() {
		clockNow, queryClock, setSystemClock, ntpSynchronized, enableNTP, installTimeSync = oldNow, oldQuery, oldSet, oldNTP, oldEnable, oldInstall
		timeSyncDidInstall, lastTimeInstall, clockGOOS = oldInstalled, oldTry, oldOS
	}
}

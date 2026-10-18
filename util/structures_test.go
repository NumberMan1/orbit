package util

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestPollTimerPauseResumeAndRepeat(t *testing.T) {
	var timer PollTimer
	start := time.Unix(0, 0)
	if err := timer.Reset(start, 10*time.Second, 0); err != nil {
		t.Fatal(err)
	}
	if !timer.Pause(start.Add(3*time.Second)) || timer.Poll(start.Add(time.Hour)) {
		t.Fatal("pause failed")
	}
	if !timer.Resume(start.Add(time.Hour)) || timer.Poll(start.Add(time.Hour+6*time.Second)) {
		t.Fatal("remaining duration lost")
	}
	if !timer.Poll(start.Add(time.Hour+7*time.Second)) || timer.Poll(start.Add(2*time.Hour)) {
		t.Fatal("once timer repeated")
	}
	timer.Reset(start, 0, time.Second)
	if !timer.Poll(start) || !timer.Poll(start.Add(time.Hour)) || timer.Poll(start.Add(time.Hour)) {
		t.Fatal("periodic coalescing failed")
	}
	timer.Cancel()
	if timer.Active() || timer.Poll(start.Add(2*time.Hour)) {
		t.Fatal("cancel failed")
	}
}
func TestBitmapCountBoundsAndOwnership(t *testing.T) {
	b := NewBitmap(10)
	if !b.Add(9) || b.Add(9) || b.Add(10) || b.Len() != 1 {
		t.Fatal("bad addition")
	}
	if b.Remove(8) || !b.Remove(9) || b.Remove(9) || b.Len() != 0 {
		t.Fatal("bad removal")
	}
	if err := b.Load([]byte{255, 3}); err != nil || b.Len() != 10 {
		t.Fatal(err, b.Len())
	}
	copy := b.Bytes()
	copy[0] = 0
	if !b.Has(0) {
		t.Fatal("aliased bytes")
	}
	if err := b.Load([]byte{0, 4}); err == nil || b.Len() != 10 {
		t.Fatal("padding validation not atomic")
	}
	input := []byte{1, 0}
	b.Load(input)
	input[0] = 0
	if !b.Has(0) || b.Len() != 1 {
		t.Fatal("aliased input")
	}
	b.Clear()
	if b.Len() != 0 {
		t.Fatal(b.Len())
	}
	if err := NewBitmap(0).Load(nil); err != nil {
		t.Fatal(err)
	}
}
func TestMatcherOverlapsUnicodeAndPreservesText(t *testing.T) {
	m := NewMatcher([]string{"aba", "ba", "坏词", "坏", ""})
	if got := m.Find("ababa"); !reflect.DeepEqual(got, []Match{{Start: 0, End: 3}, {Start: 1, End: 3}, {Start: 2, End: 5}, {Start: 3, End: 5}}) {
		t.Fatal(got)
	}
	if got := m.Replace("  坏词 & ababa!  ", '*'); got != "  ** & *****!  " {
		t.Fatal(got)
	}
	if m.Contains("not present") || !m.Contains("末尾坏词") {
		t.Fatal("match detection")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				if m.Replace("坏词", '*') != "**" {
					t.Error("concurrent search")
				}
			}
		})
	}
	wg.Wait()
}

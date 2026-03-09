package main

import (
	"testing"
	"time"
)

func TestLatestScheduleAtOrBefore(t *testing.T) {
	loc := time.FixedZone("UTC-8", -8*60*60)
	s := &scheduler{cfg: config{SummaryWeekday: time.Monday, SummaryHour: 9, SummaryMinute: 30}, location: loc}

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "same day after schedule",
			now:  time.Date(2026, 1, 12, 11, 0, 0, 0, loc),
			want: time.Date(2026, 1, 12, 9, 30, 0, 0, loc),
		},
		{
			name: "same day before schedule picks prior week",
			now:  time.Date(2026, 1, 12, 8, 0, 0, 0, loc),
			want: time.Date(2026, 1, 5, 9, 30, 0, 0, loc),
		},
		{
			name: "different weekday",
			now:  time.Date(2026, 1, 14, 12, 0, 0, 0, loc),
			want: time.Date(2026, 1, 12, 9, 30, 0, 0, loc),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.latestScheduleAtOrBefore(tc.now); !got.Equal(tc.want) {
				t.Fatalf("latestScheduleAtOrBefore() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNextScheduleAfter(t *testing.T) {
	loc := time.UTC
	s := &scheduler{cfg: config{SummaryWeekday: time.Friday, SummaryHour: 18, SummaryMinute: 0}, location: loc}
	now := time.Date(2026, 2, 7, 1, 0, 0, 0, loc)
	got := s.nextScheduleAfter(now)
	want := time.Date(2026, 2, 13, 18, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("nextScheduleAfter() = %v, want %v", got, want)
	}
}

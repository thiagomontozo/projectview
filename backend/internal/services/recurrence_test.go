package services

import (
	"testing"
	"time"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 9, 0, 0, 0, time.UTC)
}

func TestNextOccurrenceDailyAndWeekly(t *testing.T) {
	start := date(2026, time.March, 10)

	if got := NextOccurrence(start, FreqDaily, 1); !got.Equal(date(2026, time.March, 11)) {
		t.Errorf("daily = %s, want 11 March", got.Format("2 January 2006"))
	}
	if got := NextOccurrence(start, FreqWeekly, 1); !got.Equal(date(2026, time.March, 17)) {
		t.Errorf("weekly = %s, want 17 March", got.Format("2 January 2006"))
	}
	if got := NextOccurrence(start, FreqWeekly, 2); !got.Equal(date(2026, time.March, 24)) {
		t.Errorf("fortnightly = %s, want 24 March", got.Format("2 January 2006"))
	}

	// The weekday has to survive, or a "every Tuesday" task drifts across the
	// week and stops being the thing somebody scheduled.
	if got := NextOccurrence(start, FreqWeekly, 1); got.Weekday() != start.Weekday() {
		t.Errorf("weekly changed the weekday: %s -> %s", start.Weekday(), got.Weekday())
	}

	// The time of day is part of a due date and must not be reset to midnight.
	if got := NextOccurrence(start, FreqDaily, 1); got.Hour() != 9 {
		t.Errorf("the time of day was lost: %s", got)
	}
}

// The case that motivates clamping. Go's AddDate normalises 31 February
// *forward* into March, so a monthly task set on the 31st would walk a few days
// later every short month and eventually stop being monthly at all.
func TestMonthlyClampsRatherThanOverflowing(t *testing.T) {
	cases := []struct {
		name string
		from time.Time
		want time.Time
	}{
		{"31 Jan is 28 Feb, not 3 March", date(2026, time.January, 31), date(2026, time.February, 28)},
		{"31 Mar is 30 April", date(2026, time.March, 31), date(2026, time.April, 30)},
		{"30 Jan is 28 Feb", date(2026, time.January, 30), date(2026, time.February, 28)},
		{"an ordinary date is untouched", date(2026, time.March, 15), date(2026, time.April, 15)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextOccurrence(tc.from, FreqMonthly, 1); !got.Equal(tc.want) {
				t.Errorf("got %s, want %s", got.Format("2 January 2006"), tc.want.Format("2 January 2006"))
			}
		})
	}

	// 2028 is a leap year: the same rule has to find 29 days, not 28.
	if got := NextOccurrence(date(2028, time.January, 31), FreqMonthly, 1); got.Day() != 29 {
		t.Errorf("leap February = %s, want the 29th", got.Format("2 January 2006"))
	}
}

// Clamping must not become a ratchet: once a series is pulled back to the 28th
// it would stay there forever if the next step counted from the clamped date.
// The rule below is the honest consequence of storing only the last date, and
// the test pins it so the behaviour is a decision rather than a surprise.
func TestClampingIsRememberedAsTheNewDate(t *testing.T) {
	jan31 := date(2026, time.January, 31)
	feb := NextOccurrence(jan31, FreqMonthly, 1)
	mar := NextOccurrence(feb, FreqMonthly, 1)

	if feb.Day() != 28 {
		t.Fatalf("February = %s", feb.Format("2 January 2006"))
	}
	// March has 31 days, but the series now runs from the 28th.
	if mar.Day() != 28 {
		t.Errorf("March = %s; the series is expected to continue from the clamped day",
			mar.Format("2 January 2006"))
	}
}

// A neglected series must not dump every missed instance at once. Six weeks of
// an undone weekly report is one open task and one next date, not six new rows
// on top of the one nobody did.
func TestNextAfterSkipsMissedDatesInsteadOfCatchingUp(t *testing.T) {
	lastDue := date(2026, time.January, 5)
	now := date(2026, time.February, 16) // six weeks later

	next := NextAfter(lastDue, now, FreqWeekly, 1)

	if !next.After(now) {
		t.Fatalf("next = %s, which is not after %s", next, now)
	}
	// The very next weekly slot after "now", not the one right after lastDue.
	if want := date(2026, time.February, 23); !next.Equal(want) {
		t.Errorf("next = %s, want %s", next.Format("2 January 2006"), want.Format("2 January 2006"))
	}
	// And it stays on the same weekday.
	if next.Weekday() != lastDue.Weekday() {
		t.Errorf("weekday drifted: %s -> %s", lastDue.Weekday(), next.Weekday())
	}
}

func TestNextAfterTerminates(t *testing.T) {
	// A zero interval is normalised to one rather than looping forever.
	done := make(chan time.Time, 1)
	go func() {
		done <- NextAfter(date(2026, time.January, 1), date(2026, time.March, 1), FreqDaily, 0)
	}()
	select {
	case got := <-done:
		if !got.After(date(2026, time.March, 1)) {
			t.Errorf("got %s, which is not in the future", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("NextAfter did not terminate on a zero interval")
	}
}

func TestValidation(t *testing.T) {
	for _, f := range []string{FreqDaily, FreqWeekly, FreqMonthly} {
		if !ValidFrequency(f) {
			t.Errorf("%s should be valid", f)
		}
	}
	for _, f := range []string{"", "yearly", "hourly", "DAILY"} {
		if ValidFrequency(f) {
			t.Errorf("%q should be refused", f)
		}
	}
	if !ValidRecurrenceMode(ModeOnComplete) || !ValidRecurrenceMode(ModeOnSchedule) {
		t.Error("the two supported modes should be valid")
	}
	if ValidRecurrenceMode("whenever") {
		t.Error("an unknown mode should be refused")
	}
}

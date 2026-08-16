package services

import "time"

// Recurrence date arithmetic.
//
// Pure and separate from storage, because the interesting part is not the SQL:
// it is what "monthly" means on the 31st, and what "next" means for a task
// three weeks overdue. Both have wrong answers that look right until somebody
// notices their report is dated the 3rd.

const (
	FreqDaily   = "daily"
	FreqWeekly  = "weekly"
	FreqMonthly = "monthly"

	ModeOnComplete = "on_complete"
	ModeOnSchedule = "on_schedule"
)

func ValidFrequency(f string) bool {
	return f == FreqDaily || f == FreqWeekly || f == FreqMonthly
}

func ValidRecurrenceMode(m string) bool {
	return m == ModeOnComplete || m == ModeOnSchedule
}

// NextOccurrence advances one step from a date.
//
// Monthly clamps rather than overflowing. Adding a month to 31 January with
// Go's AddDate gives 3 March, because it normalises the impossible 31 February
// forward - so a monthly task set on the 31st would walk into March, then April,
// drifting a few days every short month. Clamping to the last day of the target
// month keeps "the 31st" meaning "the end of the month", which is what somebody
// picking it meant.
func NextOccurrence(from time.Time, frequency string, interval int) time.Time {
	if interval < 1 {
		interval = 1
	}

	switch frequency {
	case FreqDaily:
		return from.AddDate(0, 0, interval)
	case FreqWeekly:
		return from.AddDate(0, 0, 7*interval)
	case FreqMonthly:
		return addMonthsClamped(from, interval)
	default:
		// An unknown frequency never reaches here - the column has a CHECK and
		// the handler validates - but returning the input unchanged would make
		// a caller loop forever on a date that never advances.
		return from.AddDate(0, 0, interval)
	}
}

func addMonthsClamped(from time.Time, months int) time.Time {
	year, month, day := from.Date()
	target := time.Date(year, month, 1, from.Hour(), from.Minute(), from.Second(),
		from.Nanosecond(), from.Location()).AddDate(0, months, 0)

	if last := daysInMonth(target.Year(), target.Month()); day > last {
		day = last
	}
	return time.Date(target.Year(), target.Month(), day, from.Hour(), from.Minute(),
		from.Second(), from.Nanosecond(), from.Location())
}

// daysInMonth uses the zero-th day of the following month, which Go normalises
// to the last day of the one asked about - leap years included, with no table.
func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// NextAfter advances repeatedly until the result is strictly later than `after`.
//
// This is what keeps a neglected series from spawning a backlog of dated
// instances all at once. A weekly task last due six weeks ago has six missed
// dates; catching up by creating six tasks would bury the person who was
// already not doing one. The series moves to the next date that is genuinely in
// the future, and the instance nobody finished stays open and overdue - which
// is the honest record of what happened.
//
// Bounded so a corrupt interval cannot spin: a series more than a few thousand
// steps behind is broken, not late.
func NextAfter(from time.Time, after time.Time, frequency string, interval int) time.Time {
	next := NextOccurrence(from, frequency, interval)
	for i := 0; i < 5000 && !next.After(after); i++ {
		next = NextOccurrence(next, frequency, interval)
	}
	return next
}

// ShiftBy returns the same offset applied to a date, used to carry a task's
// start date along with its due date so the next instance keeps its duration.
func ShiftBy(t time.Time, delta time.Duration) time.Time { return t.Add(delta) }

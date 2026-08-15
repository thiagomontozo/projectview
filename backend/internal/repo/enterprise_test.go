package repo

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestKeyResultProgressMeasuresFromTheStartingValue(t *testing.T) {
	// Moving a number from 80 to 90 is at 0% on the day it is written, not at
	// 89%: measuring from zero would report almost every goal as nearly done
	// before anyone had touched it.
	if got := KeyResultProgress(80, 90, 80); got != 0 {
		t.Errorf("a measure at its starting value is at 0%%, got %v", got)
	}
	if got := KeyResultProgress(80, 90, 85); got != 0.5 {
		t.Errorf("halfway should be 0.5, got %v", got)
	}
	if got := KeyResultProgress(80, 90, 90); got != 1 {
		t.Errorf("reaching the target should be 1, got %v", got)
	}
}

func TestKeyResultProgressIsClamped(t *testing.T) {
	if got := KeyResultProgress(0, 10, 25); got != 1 {
		t.Errorf("overshooting stays at 1, got %v", got)
	}
	if got := KeyResultProgress(0, 10, -5); got != 0 {
		t.Errorf("going backwards floors at 0, got %v", got)
	}
}

func TestKeyResultProgressCountsDownwards(t *testing.T) {
	// "Cut open bugs from 40 to 10" is a legitimate key result and the target
	// is below the start, so the span is negative.
	if got := KeyResultProgress(40, 10, 25); got != 0.5 {
		t.Errorf("halfway down should be 0.5, got %v", got)
	}
	if got := KeyResultProgress(40, 10, 40); got != 0 {
		t.Errorf("no movement is 0, got %v", got)
	}
}

func TestKeyResultProgressWithNoSpan(t *testing.T) {
	// A target equal to the start has no scale to measure against; the only
	// honest answers are "not there" and "there".
	if got := KeyResultProgress(5, 5, 4); got != 0 {
		t.Errorf("below an unreachable target is 0, got %v", got)
	}
	if got := KeyResultProgress(5, 5, 5); got != 1 {
		t.Errorf("meeting it is 1, got %v", got)
	}
}

func TestGoalProgressAveragesItsMeasures(t *testing.T) {
	goal := []KeyResult{
		{StartValue: 0, TargetValue: 100, CurrentValue: 100}, // 1
		{StartValue: 0, TargetValue: 100, CurrentValue: 0},   // 0
	}
	if got := GoalProgress(goal); got != 0.5 {
		t.Errorf("expected 0.5, got %v", got)
	}
	if got := GoalProgress(nil); got != 0 {
		t.Errorf("a goal with no measures reports 0, got %v", got)
	}
}

// --- Earned value ----------------------------------------------------------

func day(n int) time.Time {
	return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC)
}

func ptr(t time.Time) *time.Time { return &t }

func TestEarnedValueOnPlan(t *testing.T) {
	start, due := day(1), day(11)
	id := uuid.New()
	baseline := []BaselineTask{{TaskID: id, Estimate: 10, StartDate: ptr(start), DueDate: ptr(due)}}

	// Halfway through the window, nothing finished, five hours spent: the plan
	// expected five hours of value and got none.
	progress := map[uuid.UUID]TaskProgress{id: {ActualHours: 5}}
	ev := ComputeEarnedValue(baseline, progress, day(6))

	if ev.BAC != 10 {
		t.Errorf("BAC should be the whole plan, got %v", ev.BAC)
	}
	if math.Abs(ev.PV-5) > 0.001 {
		t.Errorf("PV should accrue linearly to 5, got %v", ev.PV)
	}
	if ev.EV != 0 {
		t.Errorf("nothing finished earns nothing, got %v", ev.EV)
	}
	if ev.AC != 5 {
		t.Errorf("AC should be the hours tracked, got %v", ev.AC)
	}
	if ev.SV != -5 {
		t.Errorf("schedule variance should be -5, got %v", ev.SV)
	}
	if ev.SPI == nil || *ev.SPI != 0 {
		t.Errorf("SPI should be 0 when nothing is earned, got %v", ev.SPI)
	}
}

func TestEarnedValueUsesTheZeroHundredRule(t *testing.T) {
	start, due := day(1), day(11)
	id := uuid.New()
	baseline := []BaselineTask{{TaskID: id, Estimate: 10, StartDate: ptr(start), DueDate: ptr(due)}}

	// Finished early and under: the whole budget is earned the moment the task
	// is done, not in proportion to the hours spent.
	completed := day(4)
	progress := map[uuid.UUID]TaskProgress{id: {CompletedAt: ptr(completed), ActualHours: 4}}
	ev := ComputeEarnedValue(baseline, progress, day(6))

	if ev.EV != 10 {
		t.Errorf("a finished task earns its whole budget, got %v", ev.EV)
	}
	if ev.CPI == nil || math.Abs(*ev.CPI-2.5) > 0.001 {
		t.Errorf("CPI should be 10/4, got %v", ev.CPI)
	}
	if ev.EAC == nil || math.Abs(*ev.EAC-4) > 0.001 {
		t.Errorf("EAC should extrapolate to 4, got %v", ev.EAC)
	}
	if ev.VAC == nil || math.Abs(*ev.VAC-6) > 0.001 {
		t.Errorf("VAC should be 6 hours under, got %v", ev.VAC)
	}
}

func TestEarnedValueIgnoresCompletionAfterTheReportingDate(t *testing.T) {
	// Asking "where were we last Friday" must not credit work finished since.
	id := uuid.New()
	baseline := []BaselineTask{{TaskID: id, Estimate: 8, DueDate: ptr(day(10))}}
	later := day(9)
	progress := map[uuid.UUID]TaskProgress{id: {CompletedAt: ptr(later), ActualHours: 8}}

	ev := ComputeEarnedValue(baseline, progress, day(5))
	if ev.EV != 0 {
		t.Errorf("work finished after the as-of date has not been earned yet, got %v", ev.EV)
	}
}

func TestEarnedValueKeepsTheBudgetOfDeletedTasks(t *testing.T) {
	// A task removed since the baseline was taken keeps its budget: dropping
	// it would quietly shrink the plan and flatter every index.
	kept, deleted := uuid.New(), uuid.New()
	baseline := []BaselineTask{
		{TaskID: kept, Estimate: 5, DueDate: ptr(day(2))},
		{TaskID: deleted, Estimate: 5, DueDate: ptr(day(2))},
	}
	progress := map[uuid.UUID]TaskProgress{kept: {CompletedAt: ptr(day(1)), ActualHours: 5}}

	ev := ComputeEarnedValue(baseline, progress, day(3))
	if ev.BAC != 10 {
		t.Errorf("BAC keeps the deleted task, got %v", ev.BAC)
	}
	if ev.EV != 5 {
		t.Errorf("only the surviving task earns, got %v", ev.EV)
	}
	if ev.SPI == nil || *ev.SPI != 0.5 {
		t.Errorf("SPI should show the gap, got %v", ev.SPI)
	}
}

func TestPlannedValueWithoutDates(t *testing.T) {
	// A task with no due date has no schedule to be measured against, so it
	// contributes nothing to PV - but it is still part of the budget.
	id := uuid.New()
	baseline := []BaselineTask{{TaskID: id, Estimate: 12}}
	ev := ComputeEarnedValue(baseline, map[uuid.UUID]TaskProgress{}, day(30))

	if ev.PV != 0 {
		t.Errorf("an undated task owes nothing, got %v", ev.PV)
	}
	if ev.BAC != 12 {
		t.Errorf("it still counts towards the budget, got %v", ev.BAC)
	}
	if ev.SPI != nil {
		t.Error("SPI must be absent rather than dividing by a zero PV")
	}
}

func TestEarnedValueIndicesAreAbsentRatherThanZero(t *testing.T) {
	// Nothing spent: reporting CPI as 0 would read as "catastrophically over
	// budget" when the truth is "no data yet".
	id := uuid.New()
	baseline := []BaselineTask{{TaskID: id, Estimate: 4, DueDate: ptr(day(2))}}
	ev := ComputeEarnedValue(baseline, map[uuid.UUID]TaskProgress{id: {}}, day(3))

	if ev.CPI != nil {
		t.Errorf("CPI has no meaning with no actual cost, got %v", *ev.CPI)
	}
	if ev.EAC != nil {
		t.Error("EAC cannot be forecast from no cost data")
	}
}

// --- Portfolio health ------------------------------------------------------

func TestHealthReportsDoneOnlyWhenEverythingIs(t *testing.T) {
	now := day(10)
	p := PortfolioProject{TotalTasks: 4, DoneTasks: 4, EndDate: ptr(day(1))}
	if got := Health(p, now); got != "done" {
		t.Errorf("a finished project is done even if it ran late, got %q", got)
	}
	p.DoneTasks = 3
	if got := Health(p, now); got != "off_track" {
		t.Errorf("unfinished past its end date is off track, got %q", got)
	}
}

func TestHealthToleratesASingleStaleTask(t *testing.T) {
	// One overdue task in fifty is noise. Painting the project red for it
	// trains everyone to ignore the colour.
	now := day(10)
	p := PortfolioProject{TotalTasks: 50, DoneTasks: 10, OverdueOpen: 1}
	if got := Health(p, now); got != "on_track" {
		t.Errorf("expected on_track, got %q", got)
	}
	p.OverdueOpen = 20
	if got := Health(p, now); got != "at_risk" {
		t.Errorf("a fifth overdue is at risk, got %q", got)
	}
}

func TestHealthLetsTheWorstSignalWin(t *testing.T) {
	now := day(10)
	// Late and over budget at once should not read as merely at risk.
	p := PortfolioProject{TotalTasks: 10, DoneTasks: 1, OverdueOpen: 5, Estimated: 100, Tracked: 200}
	if got := Health(p, now); got != "off_track" {
		t.Errorf("expected off_track, got %q", got)
	}
	// Over budget alone is a warning, not a failure.
	p.OverdueOpen = 0
	if got := Health(p, now); got != "at_risk" {
		t.Errorf("expected at_risk, got %q", got)
	}
}

func TestHealthAllowsSmallOverruns(t *testing.T) {
	// Ten per cent over an estimate is an estimate, not an overrun.
	now := day(10)
	p := PortfolioProject{TotalTasks: 10, DoneTasks: 5, Estimated: 100, Tracked: 105}
	if got := Health(p, now); got != "on_track" {
		t.Errorf("expected on_track, got %q", got)
	}
}

// --- Service token scopes --------------------------------------------------

func TestServiceTokenScopes(t *testing.T) {
	token := ServiceToken{Scopes: []string{ScopeSCIM}}
	if !token.HasScope(ScopeSCIM) {
		t.Error("the token should carry the scope it was issued with")
	}
	if token.HasScope(ScopeReports) {
		t.Error("a token must not hold a scope nobody granted it")
	}
	if ValidScope("everything") {
		t.Error("an unknown scope must be refused rather than stored")
	}
}

import { describe, expect, it } from 'vitest';
import { isOverdue, priorityTone, toDateInput } from '../lib/format';

describe('isOverdue', () => {
  const yesterday = new Date(Date.now() - 86_400_000).toISOString();
  const tomorrow = new Date(Date.now() + 86_400_000).toISOString();

  it('flags a past due date on an unfinished task', () => {
    expect(isOverdue(yesterday, 'in_progress')).toBe(true);
  });

  // A completed task is never late, however long it took — showing "overdue"
  // on finished work is noise the board does not need.
  it('never flags a completed task', () => {
    expect(isOverdue(yesterday, 'done')).toBe(false);
  });

  it('does not flag a future due date', () => {
    expect(isOverdue(tomorrow, 'todo')).toBe(false);
  });

  it('treats a missing or unparseable date as not overdue', () => {
    expect(isOverdue(undefined, 'todo')).toBe(false);
    expect(isOverdue(null, 'todo')).toBe(false);
    expect(isOverdue('not-a-date', 'todo')).toBe(false);
  });
});

describe('toDateInput', () => {
  it('converts an ISO timestamp to the yyyy-mm-dd a date input expects', () => {
    expect(toDateInput('2026-08-15T13:45:00.000Z')).toBe('2026-08-15');
  });

  it('returns an empty string for missing or invalid values', () => {
    expect(toDateInput(undefined)).toBe('');
    expect(toDateInput(null)).toBe('');
    expect(toDateInput('nonsense')).toBe('');
  });
});

describe('priorityTone', () => {
  it('escalates the colour with the priority', () => {
    expect(priorityTone('urgent')).toBe('danger');
    expect(priorityTone('high')).toBe('warning');
    expect(priorityTone('medium')).toBe('accent');
    expect(priorityTone('low')).toBe('neutral');
  });

  it('falls back to neutral for an unknown value', () => {
    expect(priorityTone('whatever')).toBe('neutral');
  });
});

import { useCallback, useEffect, useMemo, useState } from 'react';
import type { Task } from '../types';
import { isOverdue } from '../lib/format';

export type ViewKind = 'board' | 'list' | 'table' | 'calendar' | 'timeline' | 'workload';
export type GroupBy = 'none' | 'status' | 'assignee' | 'priority';
export type SortBy = 'position' | 'title' | 'dueDate' | 'priority' | 'status';
export type SortDirection = 'asc' | 'desc';

export interface ViewFilters {
  search: string;
  status: string[];
  priority: string[];
  assignees: string[];
  overdueOnly: boolean;
}

export interface ViewState {
  kind: ViewKind;
  groupBy: GroupBy;
  sortBy: SortBy;
  sortDirection: SortDirection;
  filters: ViewFilters;
}

const EMPTY_FILTERS: ViewFilters = {
  search: '',
  status: [],
  priority: [],
  assignees: [],
  overdueOnly: false
};

const DEFAULT_STATE: ViewState = {
  kind: 'board',
  groupBy: 'status',
  sortBy: 'position',
  sortDirection: 'asc',
  filters: EMPTY_FILTERS
};

/**
 * Per-project view preference.
 *
 * Persisted locally rather than on the server: which view someone last used is
 * a preference of *this browser*, and round-tripping it would mean a request
 * before the board can even render. Scoped by project id, because the view
 * that suits a roadmap is rarely the one that suits a bug list.
 */
export function useViewState(projectId: string | undefined) {
  const storageKey = projectId ? `pv_view_${projectId}` : null;

  const [state, setState] = useState<ViewState>(() => {
    if (!storageKey) return DEFAULT_STATE;
    try {
      const stored = localStorage.getItem(storageKey);
      if (!stored) return DEFAULT_STATE;
      const parsed = JSON.parse(stored) as Partial<ViewState>;
      return {
        ...DEFAULT_STATE,
        ...parsed,
        // Filters are never restored: reopening a board to a filtered subset
        // with no obvious reason looks like missing data.
        filters: EMPTY_FILTERS
      };
    } catch {
      return DEFAULT_STATE;
    }
  });

  useEffect(() => {
    if (!storageKey) return;
    try {
      const { filters, ...persisted } = state;
      void filters;
      localStorage.setItem(storageKey, JSON.stringify(persisted));
    } catch {
      /* storage unavailable; the choice lasts for this session */
    }
  }, [state, storageKey]);

  const setKind = useCallback((kind: ViewKind) => setState((s) => ({ ...s, kind })), []);
  const setGroupBy = useCallback((groupBy: GroupBy) => setState((s) => ({ ...s, groupBy })), []);

  const setSort = useCallback(
    (sortBy: SortBy) =>
      setState((s) => ({
        ...s,
        sortBy,
        // Clicking the active column flips direction, which is what a table
        // header is expected to do.
        sortDirection: s.sortBy === sortBy && s.sortDirection === 'asc' ? 'desc' : 'asc'
      })),
    []
  );

  const setFilters = useCallback(
    (update: Partial<ViewFilters>) => setState((s) => ({ ...s, filters: { ...s.filters, ...update } })),
    []
  );

  const clearFilters = useCallback(() => setState((s) => ({ ...s, filters: EMPTY_FILTERS })), []);

  const activeFilterCount = useMemo(() => {
    const { search, status, priority, assignees, overdueOnly } = state.filters;
    return (
      (search.trim() ? 1 : 0) +
      status.length +
      priority.length +
      assignees.length +
      (overdueOnly ? 1 : 0)
    );
  }, [state.filters]);

  return { state, setKind, setGroupBy, setSort, setFilters, clearFilters, activeFilterCount };
}

const PRIORITY_ORDER: Record<string, number> = { urgent: 0, high: 1, medium: 2, low: 3 };

/** Applies the filters, then the sort. Pure, so it is straightforward to test. */
export function applyView(tasks: Task[], state: ViewState, statusOrder: string[]): Task[] {
  const { search, status, priority, assignees, overdueOnly } = state.filters;
  const needle = search.trim().toLowerCase();

  const filtered = tasks.filter((task) => {
    if (needle) {
      const haystack = `${task.title} ${task.description ?? ''}`.toLowerCase();
      if (!haystack.includes(needle)) return false;
    }
    if (status.length && !status.includes(task.status)) return false;
    if (priority.length && !priority.includes(task.priority)) return false;
    if (assignees.length && !task.assignees.some((a) => assignees.includes(a.id))) return false;
    if (overdueOnly && !isOverdue(task.dueDate, task.status)) return false;
    return true;
  });

  const direction = state.sortDirection === 'asc' ? 1 : -1;

  return filtered.sort((a, b) => {
    switch (state.sortBy) {
      case 'title':
        return a.title.localeCompare(b.title) * direction;
      case 'dueDate': {
        // Tasks without a due date sort last in either direction: they are
        // unscheduled, not "infinitely early".
        if (!a.dueDate && !b.dueDate) return 0;
        if (!a.dueDate) return 1;
        if (!b.dueDate) return -1;
        return (new Date(a.dueDate).getTime() - new Date(b.dueDate).getTime()) * direction;
      }
      case 'priority':
        return ((PRIORITY_ORDER[a.priority] ?? 9) - (PRIORITY_ORDER[b.priority] ?? 9)) * direction;
      case 'status':
        return (statusOrder.indexOf(a.status) - statusOrder.indexOf(b.status)) * direction;
      default:
        return (a.order - b.order) * direction;
    }
  });
}

export interface TaskGroup {
  key: string;
  label: string;
  color?: string;
  tasks: Task[];
}

/** Buckets tasks for the grouped views. */
export function groupTasks(
  tasks: Task[],
  groupBy: GroupBy,
  statuses: Array<{ key: string; label: string; color: string }>,
  labels: { ungrouped: string; unassigned: string; priority: (p: string) => string }
): TaskGroup[] {
  if (groupBy === 'none') {
    return [{ key: 'all', label: labels.ungrouped, tasks }];
  }

  if (groupBy === 'status') {
    return statuses.map((status) => ({
      key: status.key,
      label: status.label,
      color: status.color,
      tasks: tasks.filter((task) => task.status === status.key)
    }));
  }

  if (groupBy === 'priority') {
    return ['urgent', 'high', 'medium', 'low'].map((priority) => ({
      key: priority,
      label: labels.priority(priority),
      tasks: tasks.filter((task) => task.priority === priority)
    }));
  }

  // By assignee. A task with several assignees appears under each of them,
  // which is what "show me their work" means - the alternative would hide
  // shared tasks from everyone but the first name on the list.
  const groups = new Map<string, TaskGroup>();
  const unassigned: Task[] = [];

  tasks.forEach((task) => {
    if (task.assignees.length === 0) {
      unassigned.push(task);
      return;
    }
    task.assignees.forEach((assignee) => {
      const existing = groups.get(assignee.id);
      if (existing) {
        existing.tasks.push(task);
      } else {
        groups.set(assignee.id, {
          key: assignee.id,
          label: assignee.name,
          color: assignee.avatarColor,
          tasks: [task]
        });
      }
    });
  });

  const result = [...groups.values()].sort((a, b) => a.label.localeCompare(b.label));
  if (unassigned.length) {
    result.push({ key: 'unassigned', label: labels.unassigned, tasks: unassigned });
  }
  return result;
}

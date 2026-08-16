import { useCallback, useEffect, useMemo, useState } from 'react';
import type { Task } from '../types';

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

/**
 * Filtering and sorting used to happen here, over the whole project loaded into
 * the browser. They now happen in SQL.
 *
 * That was not a preference. With the views paged, the client only ever holds a
 * page, so a filter applied here would have meant "within what happened to load"
 * — a board reporting three matches when there are three hundred. The rules
 * moved with the work: severity ordering for priority, the project's own column
 * order for status, and unscheduled tasks sorting last in either direction all
 * live in repo.SortableTaskColumns and taskOrderBy, and are tested there.
 *
 * Grouping stays here, because it only ever arranges what is on screen and says
 * so.
 */

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

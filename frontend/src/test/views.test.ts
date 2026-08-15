import { describe, expect, it } from 'vitest';
import { applyView, groupTasks, type ViewState } from '../views/useViewState';
import type { ProjectStatusColumn, Task } from '../types';

const STATUSES: ProjectStatusColumn[] = [
  { key: 'todo', label: 'To Do', order: 0, color: '#60a5fa' },
  { key: 'in_progress', label: 'In Progress', order: 1, color: '#f59e0b' },
  { key: 'done', label: 'Done', order: 2, color: '#22c55e' }
];

const LABELS = {
  ungrouped: 'All tasks',
  unassigned: 'Unassigned',
  priority: (p: string) => p
};

function task(overrides: Partial<Task> & { id: string }): Task {
  return {
    title: 'Task',
    description: '',
    project: 'p1',
    assignees: [],
    status: 'todo',
    priority: 'medium',
    estimateHours: 0,
    order: 0,
    tags: [],
    checklist: [],
    comments: [],
    subtaskCount: 0,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides
  } as Task;
}

const baseState: ViewState = {
  kind: 'list',
  groupBy: 'status',
  sortBy: 'position',
  sortDirection: 'asc',
  filters: { search: '', status: [], priority: [], assignees: [], overdueOnly: false }
};

const statusOrder = STATUSES.map((s) => s.key);

describe('applyView — filtering', () => {
  const tasks = [
    task({ id: '1', title: 'Deploy the API', status: 'todo', priority: 'high' }),
    task({ id: '2', title: 'Write docs', status: 'done', priority: 'low' }),
    task({ id: '3', title: 'Fix deploy script', status: 'in_progress', priority: 'urgent' })
  ];

  it('matches the search against title and description', () => {
    const result = applyView(tasks, { ...baseState, filters: { ...baseState.filters, search: 'deploy' } }, statusOrder);
    expect(result.map((t) => t.id)).toEqual(['1', '3']);
  });

  it('ignores case and surrounding whitespace in the search', () => {
    const result = applyView(tasks, { ...baseState, filters: { ...baseState.filters, search: '  DOCS ' } }, statusOrder);
    expect(result.map((t) => t.id)).toEqual(['2']);
  });

  it('filters by status', () => {
    const result = applyView(tasks, { ...baseState, filters: { ...baseState.filters, status: ['done'] } }, statusOrder);
    expect(result.map((t) => t.id)).toEqual(['2']);
  });

  it('treats several selected values as OR within a facet', () => {
    const result = applyView(
      tasks,
      { ...baseState, filters: { ...baseState.filters, priority: ['high', 'urgent'] } },
      statusOrder
    );
    expect(result.map((t) => t.id)).toEqual(['1', '3']);
  });

  it('combines different facets as AND', () => {
    const result = applyView(
      tasks,
      { ...baseState, filters: { ...baseState.filters, search: 'deploy', status: ['todo'] } },
      statusOrder
    );
    expect(result.map((t) => t.id)).toEqual(['1']);
  });

  it('keeps everything when no filter is set', () => {
    expect(applyView(tasks, baseState, statusOrder)).toHaveLength(3);
  });
});

describe('applyView — overdue filter', () => {
  const yesterday = new Date(Date.now() - 86_400_000).toISOString();

  it('keeps only unfinished tasks past their due date', () => {
    const tasks = [
      task({ id: 'late', dueDate: yesterday, status: 'todo' }),
      // Past its date but finished: not overdue, and must not be listed.
      task({ id: 'finished', dueDate: yesterday, status: 'done' }),
      task({ id: 'undated', status: 'todo' })
    ];

    const result = applyView(
      tasks,
      { ...baseState, filters: { ...baseState.filters, overdueOnly: true } },
      statusOrder
    );
    expect(result.map((t) => t.id)).toEqual(['late']);
  });
});

describe('applyView — sorting', () => {
  it('orders by priority, most urgent first', () => {
    const tasks = [
      task({ id: 'low', priority: 'low' }),
      task({ id: 'urgent', priority: 'urgent' }),
      task({ id: 'medium', priority: 'medium' })
    ];
    const result = applyView(tasks, { ...baseState, sortBy: 'priority' }, statusOrder);
    expect(result.map((t) => t.id)).toEqual(['urgent', 'medium', 'low']);
  });

  it('reverses on descending', () => {
    const tasks = [task({ id: 'a', title: 'Alpha' }), task({ id: 'b', title: 'Beta' })];
    const result = applyView(tasks, { ...baseState, sortBy: 'title', sortDirection: 'desc' }, statusOrder);
    expect(result.map((t) => t.id)).toEqual(['b', 'a']);
  });

  // Unscheduled work is not "infinitely early" — it belongs at the end either
  // way, or sorting by date buries everything that has a date.
  it('puts tasks without a due date last in both directions', () => {
    const tasks = [
      task({ id: 'none' }),
      task({ id: 'later', dueDate: '2026-12-01T00:00:00Z' }),
      task({ id: 'sooner', dueDate: '2026-06-01T00:00:00Z' })
    ];

    expect(applyView(tasks, { ...baseState, sortBy: 'dueDate' }, statusOrder).map((t) => t.id)).toEqual([
      'sooner',
      'later',
      'none'
    ]);

    expect(
      applyView(tasks, { ...baseState, sortBy: 'dueDate', sortDirection: 'desc' }, statusOrder).map((t) => t.id)
    ).toEqual(['later', 'sooner', 'none']);
  });

  it('orders by the project\'s own status sequence, not alphabetically', () => {
    const tasks = [
      task({ id: 'done', status: 'done' }),
      task({ id: 'todo', status: 'todo' }),
      task({ id: 'doing', status: 'in_progress' })
    ];
    const result = applyView(tasks, { ...baseState, sortBy: 'status' }, statusOrder);
    expect(result.map((t) => t.id)).toEqual(['todo', 'doing', 'done']);
  });
});

describe('groupTasks', () => {
  const ana = { id: 'u1', name: 'Ana', email: 'a@x', avatarColor: '#111' };
  const bruno = { id: 'u2', name: 'Bruno', email: 'b@x', avatarColor: '#222' };

  it('groups by status in the project order, keeping empty groups', () => {
    const tasks = [task({ id: '1', status: 'todo' }), task({ id: '2', status: 'todo' })];
    const groups = groupTasks(tasks, 'status', STATUSES, LABELS);

    expect(groups.map((g) => g.key)).toEqual(['todo', 'in_progress', 'done']);
    expect(groups[0].tasks).toHaveLength(2);
    expect(groups[1].tasks).toHaveLength(0);
  });

  // Shared work has to appear for everyone on it; listing it only under the
  // first assignee would hide it from the other person's view.
  it('lists a shared task under each of its assignees', () => {
    const tasks = [task({ id: 'shared', assignees: [ana, bruno] })];
    const groups = groupTasks(tasks, 'assignee', STATUSES, LABELS);

    expect(groups.map((g) => g.label)).toEqual(['Ana', 'Bruno']);
    expect(groups[0].tasks[0].id).toBe('shared');
    expect(groups[1].tasks[0].id).toBe('shared');
  });

  it('collects unassigned work into its own group, last', () => {
    const tasks = [task({ id: 'mine', assignees: [ana] }), task({ id: 'orphan' })];
    const groups = groupTasks(tasks, 'assignee', STATUSES, LABELS);

    expect(groups.at(-1)?.label).toBe('Unassigned');
    expect(groups.at(-1)?.tasks.map((t) => t.id)).toEqual(['orphan']);
  });

  it('returns one bucket when grouping is off', () => {
    const tasks = [task({ id: '1' }), task({ id: '2' })];
    const groups = groupTasks(tasks, 'none', STATUSES, LABELS);

    expect(groups).toHaveLength(1);
    expect(groups[0].tasks).toHaveLength(2);
  });
});

import { describe, expect, it } from 'vitest';
import { groupTasks } from '../views/useViewState';
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

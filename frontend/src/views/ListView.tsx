import { useCallback, useMemo, useRef, useState } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import styles from './views.module.css';
import { Avatar, AvatarGroup, Badge } from '../ui/display';
import { formatDate, isOverdue, priorityTone } from '../lib/format';
import { groupTasks, type ViewState } from './useViewState';
import type { ProjectStatusColumn, PublicUser, Task } from '../types';

interface Props {
  tasks: Task[];
  state: ViewState;
  statuses: ProjectStatusColumn[];
  selected: Set<string>;
  onToggleSelect: (taskId: string) => void;
  onOpenTask: (task: Task) => void;
  /**
   * How many tasks each group really holds, counted by the server under the
   * same filters.
   *
   * Without this the headers counted the rows that happened to be loaded, so a
   * header read "Ana 30" when Ana had 800 - and a group with nothing on the
   * current page was hidden entirely, which reads as "nothing assigned"
   * rather than "not loaded". The first is a wrong number; the second is the
   * interface stating something untrue.
   */
  groupTotals?: Record<string, number>;
  /** Everyone who could own a group, so a person with work off-page still appears. */
  members?: PublicUser[];
  /**
   * Adds a task straight into a group.
   *
   * The group is the context somebody is already in - they are looking at "In
   * progress" and want one more thing in it - so the button carries that
   * context rather than opening a form where the first thing to do is set the
   * value that was on screen a moment ago.
   */
  onAddInGroup?: (group: { by: ViewState['groupBy']; key: string }) => void;
}

type Row =
  | {
      kind: 'group';
      key: string;
      label: string;
      color?: string;
      /** Loaded here. */
      count: number;
      /** What the server says the group really holds, when it knows. */
      total?: number;
      collapsed: boolean;
    }
  | { kind: 'task'; key: string; task: Task };

/**
 * Names a group the server knows about but this page did not load.
 *
 * Falls back to the raw key rather than inventing a friendly name: an
 * unrecognised id shown as itself is odd, and a wrong name is worse.
 */
function groupLabel(
  key: string,
  groupBy: ViewState['groupBy'],
  statuses: ProjectStatusColumn[],
  members: PublicUser[] | undefined,
  t: (key: string) => string
): string {
  if (groupBy === 'status') return statuses.find((s) => s.key === key)?.label ?? key;
  if (groupBy === 'priority') return t(`task.priority${key.charAt(0).toUpperCase()}${key.slice(1)}`);
  if (key === 'unassigned') return t('views.unassigned');
  return members?.find((m) => m.id === key)?.name ?? key;
}

/**
 * Grouped list of tasks.
 *
 * Groups and their rows are flattened into a single array so one virtualizer
 * covers the whole list: nesting a virtualizer per group would recreate the
 * problem it exists to solve, since the group headers themselves would all
 * still be mounted.
 */
export function ListView({
  tasks,
  state,
  statuses,
  selected,
  onToggleSelect,
  onOpenTask,
  groupTotals,
  members,
  onAddInGroup
}: Props) {
  const { t } = useTranslation();
  const scrollerRef = useRef<HTMLDivElement>(null);

  // Which groups are folded away. Kept here rather than in the saved view: it
  // is a reading posture, not a definition of the view, and somebody who folds
  // "Completed" to get it out of the way has not changed what the view is.
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const toggle = useCallback((key: string) => {
    setCollapsed((current) => {
      const next = new Set(current);
      if (!next.delete(key)) next.add(key);
      return next;
    });
  }, []);

  const rows = useMemo<Row[]>(() => {
    const groups = groupTasks(tasks, state.groupBy, statuses, {
      ungrouped: t('views.allTasks'),
      unassigned: t('views.unassigned'),
      priority: (p) => t(`task.priority${p.charAt(0).toUpperCase()}${p.slice(1)}`)
    });

    // groupTasks can only produce groups for the rows in hand, so a person
    // whose work is entirely off this page would still be missing - and an
    // absent name reads as "nothing assigned" rather than "not loaded". The
    // server's counts name every group that really exists, so the ones it knows
    // about and this page does not are added back, empty and honestly labelled.
    if (groupTotals && state.groupBy !== 'none') {
      const present = new Set(groups.map((group) => group.key));
      Object.keys(groupTotals).forEach((key) => {
        if (present.has(key) || !groupTotals[key]) return;
        groups.push({ key, label: groupLabel(key, state.groupBy, statuses, members, t), tasks: [] });
      });
    }

    const flat: Row[] = [];
    groups.forEach((group) => {
      const total = groupTotals?.[group.key];
      // A group with nothing loaded is only noise when the server agrees it is
      // empty. When the server says it holds work that simply is not on this
      // page, hiding it would tell the reader that person has nothing.
      if (group.tasks.length === 0 && state.groupBy !== 'none' && !total) return;
      const folded = collapsed.has(group.key);
      if (state.groupBy !== 'none') {
        flat.push({
          kind: 'group',
          key: `group-${group.key}`,
          label: group.label,
          color: group.color,
          count: group.tasks.length,
          total,
          collapsed: folded
        });
      }
      // A folded group still contributes its header, and its header still
      // carries the count - so folding hides the rows without hiding the fact
      // that they exist.
      if (folded) return;
      group.tasks.forEach((task) => flat.push({ kind: 'task', key: `${group.key}-${task.id}`, task }));
    });
    return flat;
    // groupTotals and members belong here: the counts arrive after the first
    // render, and without them the memo would keep the headers it computed
    // before the server answered - which is the wrong number this change
    // exists to remove.
  }, [tasks, state.groupBy, statuses, groupTotals, members, collapsed, t]);

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollerRef.current,
    estimateSize: (index) => (rows[index].kind === 'group' ? 44 : 42),
    overscan: 12
  });

  return (
    <div className={styles.virtualScroller} ref={scrollerRef}>
      <div style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
        {virtualizer.getVirtualItems().map((virtualRow) => {
          const row = rows[virtualRow.index];

          return (
            <div
              key={row.key}
              style={{
                position: 'absolute',
                top: 0,
                left: 0,
                width: '100%',
                height: virtualRow.size,
                transform: `translateY(${virtualRow.start}px)`
              }}
            >
              {row.kind === 'group' ? (
                <div className={styles.groupHeader}>
                  <button
                    type="button"
                    className={styles.groupToggle}
                    aria-expanded={!row.collapsed}
                    aria-label={row.collapsed ? t('views.expandGroup') : t('views.collapseGroup')}
                    onClick={() => toggle(row.key.replace(/^group-/, ''))}
                  >
                    <span aria-hidden="true">{row.collapsed ? '▸' : '▾'}</span>
                  </button>
                  {row.color && (
                    <span className={styles.groupDot} style={{ background: row.color }} aria-hidden="true" />
                  )}
                  <span className={styles.groupLabel}>{row.label}</span>
                  {/* "12 of 340" whenever the two differ, so the number beside
                      a name is never mistaken for that person's whole workload. */}
                  <span className={styles.groupCount}>
                    {row.total !== undefined && row.total !== row.count
                      ? t('views.groupCountOf', { loaded: row.count, total: row.total })
                      : row.count}
                  </span>
                  {onAddInGroup && state.groupBy !== 'none' && (
                    <button
                      type="button"
                      className={styles.groupAdd}
                      onClick={() =>
                        onAddInGroup({ by: state.groupBy, key: row.key.replace(/^group-/, '') })
                      }
                    >
                      + {t('board.newTask')}
                    </button>
                  )}
                </div>
              ) : (
                <TaskRow
                  task={row.task}
                  selected={selected.has(row.task.id)}
                  onToggleSelect={onToggleSelect}
                  onOpen={onOpenTask}
                />
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function TaskRow({
  task,
  selected,
  onToggleSelect,
  onOpen
}: {
  task: Task;
  selected: boolean;
  onToggleSelect: (id: string) => void;
  onOpen: (task: Task) => void;
}) {
  const { t } = useTranslation();
  const overdue = isOverdue(task.dueDate, task.status);

  return (
    <div className={clsx(styles.row, selected && styles.rowSelected)}>
      <input
        type="checkbox"
        className={styles.checkbox}
        checked={selected}
        onChange={() => onToggleSelect(task.id)}
        aria-label={t('views.selectTask', { title: task.title })}
      />

      <button
        type="button"
        className={styles.rowTitle}
        onClick={() => onOpen(task)}
        style={{ background: 'none', textAlign: 'left' }}
      >
        {task.title}
      </button>

      <span className={styles.rowMeta}>
        <Badge tone={priorityTone(task.priority)}>
          {t(`task.priority${task.priority.charAt(0).toUpperCase()}${task.priority.slice(1)}`)}
        </Badge>
        {task.dueDate && (
          <Badge tone={overdue ? 'danger' : 'neutral'}>{formatDate(task.dueDate)}</Badge>
        )}
        <AvatarGroup>
          {task.assignees.slice(0, 3).map((assignee) => (
            <Avatar key={assignee.id} name={assignee.name} color={assignee.avatarColor} size={22} />
          ))}
        </AvatarGroup>
      </span>
    </div>
  );
}

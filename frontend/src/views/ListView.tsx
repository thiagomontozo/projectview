import { useMemo, useRef } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import styles from './views.module.css';
import { Avatar, AvatarGroup, Badge } from '../ui/display';
import { formatDate, isOverdue, priorityTone } from '../lib/format';
import { groupTasks, type ViewState } from './useViewState';
import type { ProjectStatusColumn, Task } from '../types';

interface Props {
  tasks: Task[];
  state: ViewState;
  statuses: ProjectStatusColumn[];
  selected: Set<string>;
  onToggleSelect: (taskId: string) => void;
  onOpenTask: (task: Task) => void;
}

type Row =
  | { kind: 'group'; key: string; label: string; color?: string; count: number }
  | { kind: 'task'; key: string; task: Task };

/**
 * Grouped list of tasks.
 *
 * Groups and their rows are flattened into a single array so one virtualizer
 * covers the whole list: nesting a virtualizer per group would recreate the
 * problem it exists to solve, since the group headers themselves would all
 * still be mounted.
 */
export function ListView({ tasks, state, statuses, selected, onToggleSelect, onOpenTask }: Props) {
  const { t } = useTranslation();
  const scrollerRef = useRef<HTMLDivElement>(null);

  const rows = useMemo<Row[]>(() => {
    const groups = groupTasks(tasks, state.groupBy, statuses, {
      ungrouped: t('views.allTasks'),
      unassigned: t('views.unassigned'),
      priority: (p) => t(`task.priority${p.charAt(0).toUpperCase()}${p.slice(1)}`)
    });

    const flat: Row[] = [];
    groups.forEach((group) => {
      // An empty group is noise on a filtered list.
      if (group.tasks.length === 0 && state.groupBy !== 'none') return;
      if (state.groupBy !== 'none') {
        flat.push({
          kind: 'group',
          key: `group-${group.key}`,
          label: group.label,
          color: group.color,
          count: group.tasks.length
        });
      }
      group.tasks.forEach((task) => flat.push({ kind: 'task', key: `${group.key}-${task.id}`, task }));
    });
    return flat;
  }, [tasks, state.groupBy, statuses, t]);

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
                  {row.color && (
                    <span className={styles.groupDot} style={{ background: row.color }} aria-hidden="true" />
                  )}
                  <span className={styles.groupLabel}>{row.label}</span>
                  <span className={styles.groupCount}>{row.count}</span>
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

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import styles from './views.module.css';
import { Avatar, AvatarGroup, Badge } from '../ui/display';
import { ArrowDown, ArrowUp } from '../ui/icons';
import { formatDate, isOverdue, priorityTone, toDateInput } from '../lib/format';
import type { SortBy, ViewState } from './useViewState';
import type { ProjectStatusColumn, Task } from '../types';

interface Props {
  tasks: Task[];
  state: ViewState;
  statuses: ProjectStatusColumn[];
  selected: Set<string>;
  onToggleSelect: (taskId: string) => void;
  onToggleSelectAll: (taskIds: string[]) => void;
  onSort: (sortBy: SortBy) => void;
  onOpenTask: (task: Task) => void;
  onPatchTask: (taskId: string, patch: Record<string, unknown>) => void;
}

/**
 * Spreadsheet-style view with inline editing.
 *
 * Edits commit on blur or Enter rather than on every keystroke: a mutation per
 * character would flood the server and make the undo story incoherent. Escape
 * abandons the edit and restores the stored value.
 */
export function TableView({
  tasks,
  state,
  statuses,
  selected,
  onToggleSelect,
  onToggleSelectAll,
  onSort,
  onOpenTask,
  onPatchTask
}: Props) {
  const { t } = useTranslation();

  const allSelected = tasks.length > 0 && tasks.every((task) => selected.has(task.id));

  const columns: Array<{ key: SortBy | 'assignees' | 'select'; labelKey: string; sortable: boolean }> = [
    { key: 'select', labelKey: '', sortable: false },
    { key: 'title', labelKey: 'task.title', sortable: true },
    { key: 'status', labelKey: 'task.status', sortable: true },
    { key: 'priority', labelKey: 'task.priority', sortable: true },
    { key: 'dueDate', labelKey: 'task.due', sortable: true },
    { key: 'assignees', labelKey: 'task.assignees', sortable: false }
  ];

  return (
    <div className={styles.tableWrap}>
      <table className={styles.table}>
        <caption className="sr-only">{t('views.table')}</caption>
        <thead>
          <tr>
            {columns.map((column) => (
              <th key={column.key} scope="col" style={column.key === 'select' ? { width: 40 } : undefined}>
                {column.key === 'select' ? (
                  <input
                    type="checkbox"
                    className={styles.checkbox}
                    checked={allSelected}
                    onChange={() => onToggleSelectAll(tasks.map((task) => task.id))}
                    aria-label={t('views.selectAll')}
                  />
                ) : column.sortable ? (
                  <button
                    type="button"
                    className={styles.sortButton}
                    onClick={() => onSort(column.key as SortBy)}
                    // Announces the sort state instead of leaving it to the arrow glyph.
                    aria-sort={
                      state.sortBy === column.key
                        ? state.sortDirection === 'asc'
                          ? 'ascending'
                          : 'descending'
                        : 'none'
                    }
                  >
                    {t(column.labelKey)}
                    {state.sortBy === column.key &&
                      (state.sortDirection === 'asc' ? <ArrowUp size={12} /> : <ArrowDown size={12} />)}
                  </button>
                ) : (
                  t(column.labelKey)
                )}
              </th>
            ))}
          </tr>
        </thead>

        <tbody>
          {tasks.map((task) => (
            <tr key={task.id} className={clsx(selected.has(task.id) && styles.tableRowSelected)}>
              <td>
                <div className={styles.cell}>
                  <input
                    type="checkbox"
                    className={styles.checkbox}
                    checked={selected.has(task.id)}
                    onChange={() => onToggleSelect(task.id)}
                    aria-label={t('views.selectTask', { title: task.title })}
                  />
                </div>
              </td>

              <td>
                <EditableText
                  value={task.title}
                  label={t('task.title')}
                  onCommit={(value) => value !== task.title && onPatchTask(task.id, { title: value })}
                  onOpen={() => onOpenTask(task)}
                />
              </td>

              <td>
                <div className={styles.cell}>
                  <select
                    className={styles.cellInput}
                    value={task.status}
                    onChange={(event) => onPatchTask(task.id, { status: event.target.value })}
                    aria-label={t('task.status')}
                  >
                    {statuses.map((status) => (
                      <option key={status.key} value={status.key}>
                        {status.label}
                      </option>
                    ))}
                  </select>
                </div>
              </td>

              <td>
                <div className={styles.cell}>
                  <select
                    className={styles.cellInput}
                    value={task.priority}
                    onChange={(event) => onPatchTask(task.id, { priority: event.target.value })}
                    aria-label={t('task.priority')}
                  >
                    {['urgent', 'high', 'medium', 'low'].map((priority) => (
                      <option key={priority} value={priority}>
                        {t(`task.priority${priority.charAt(0).toUpperCase()}${priority.slice(1)}`)}
                      </option>
                    ))}
                  </select>
                </div>
              </td>

              <td>
                <div className={styles.cell}>
                  <input
                    type="date"
                    className={styles.cellInput}
                    value={toDateInput(task.dueDate)}
                    onChange={(event) =>
                      onPatchTask(task.id, {
                        dueDate: event.target.value ? new Date(event.target.value).toISOString() : null
                      })
                    }
                    aria-label={t('task.due')}
                  />
                  {isOverdue(task.dueDate, task.status) && (
                    <Badge tone="danger">{t('task.overdue')}</Badge>
                  )}
                </div>
              </td>

              <td>
                <div className={styles.cell}>
                  <AvatarGroup>
                    {task.assignees.slice(0, 4).map((assignee) => (
                      <Avatar key={assignee.id} name={assignee.name} color={assignee.avatarColor} size={22} />
                    ))}
                  </AvatarGroup>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function EditableText({
  value,
  label,
  onCommit,
  onOpen
}: {
  value: string;
  label: string;
  onCommit: (value: string) => void;
  onOpen: () => void;
}) {
  const [draft, setDraft] = useState(value);
  const [editing, setEditing] = useState(false);

  // The row can change underneath an idle cell (a realtime push, another
  // view's edit), so an untouched cell follows the stored value.
  if (!editing && draft !== value) setDraft(value);

  return (
    <div className={styles.cell}>
      <input
        className={styles.cellInput}
        value={draft}
        aria-label={label}
        onFocus={() => setEditing(true)}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={() => {
          setEditing(false);
          if (draft.trim()) onCommit(draft.trim());
          else setDraft(value);
        }}
        onKeyDown={(event) => {
          if (event.key === 'Enter') event.currentTarget.blur();
          if (event.key === 'Escape') {
            setDraft(value);
            setEditing(false);
            event.currentTarget.blur();
          }
          // Opens the full task without leaving the keyboard.
          if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) onOpen();
        }}
      />
    </div>
  );
}

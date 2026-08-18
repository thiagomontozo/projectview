import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import styles from './views.module.css';
import { Avatar, AvatarGroup, Badge } from '../ui/display';
import { ArrowDown, ArrowUp } from '../ui/icons';
import { formatDate, isOverdue, priorityTone, toDateInput } from '../lib/format';
import type { SortBy, ViewState } from './useViewState';
import type { FieldDefinition } from '../lib/queries';
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
  /**
   * The project's custom fields, offered as columns.
   *
   * They existed in the API and on the task dialog, but only ever one task at a
   * time - which is the one place a custom field is least useful. A field
   * called "Client" or "Cost centre" is asked as a question about the whole
   * list, and answering it meant opening tasks one by one.
   */
  customFields?: FieldDefinition[];
  /** Which of them are shown, by key. Nothing by default: an extra column is a
   *  decision, and six of them arriving unasked is a table nobody can read. */
  shownFields?: string[];
  onShownFieldsChange?: (keys: string[]) => void;
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
  onPatchTask,
  customFields = [],
  shownFields = [],
  onShownFieldsChange
}: Props) {
  const { t } = useTranslation();
  const [picking, setPicking] = useState(false);

  const allSelected = tasks.length > 0 && tasks.every((task) => selected.has(task.id));

  const columns: Array<{ key: SortBy | 'assignees' | 'select'; labelKey: string; sortable: boolean }> = [
    { key: 'select', labelKey: '', sortable: false },
    { key: 'title', labelKey: 'task.title', sortable: true },
    { key: 'status', labelKey: 'task.status', sortable: true },
    { key: 'priority', labelKey: 'task.priority', sortable: true },
    { key: 'dueDate', labelKey: 'task.due', sortable: true },
    { key: 'assignees', labelKey: 'task.assignees', sortable: false }
  ];

  // Only fields that still exist. A column list outlives the field it names -
  // somebody deletes "Cost centre" and every row would otherwise render an
  // empty column with a heading nothing can fill.
  const extraColumns = customFields.filter((field) => shownFields.includes(field.key));

  function toggleField(key: string) {
    if (!onShownFieldsChange) return;
    onShownFieldsChange(
      shownFields.includes(key) ? shownFields.filter((k) => k !== key) : [...shownFields, key]
    );
  }

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

            {extraColumns.map((field) => (
              <th key={field.key} scope="col">
                {field.label}
              </th>
            ))}

            {customFields.length > 0 && onShownFieldsChange && (
              <th scope="col" className={styles.addColumn}>
                <button
                  type="button"
                  className={styles.sortButton}
                  aria-expanded={picking}
                  onClick={() => setPicking((open) => !open)}
                >
                  + {t('views.addColumn')}
                </button>
                {picking && (
                  <div className={styles.columnPicker} role="group" aria-label={t('views.addColumn')}>
                    {customFields.map((field) => (
                      <label key={field.key} className={styles.columnOption}>
                        <input
                          type="checkbox"
                          checked={shownFields.includes(field.key)}
                          onChange={() => toggleField(field.key)}
                        />
                        {field.label}
                      </label>
                    ))}
                  </div>
                )}
              </th>
            )}
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

              {extraColumns.map((field) => (
                <td key={field.key}>
                  <div className={styles.cell}>{customValue(task, field)}</div>
                </td>
              ))}

              {customFields.length > 0 && onShownFieldsChange && <td />}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * Renders one custom value.
 *
 * Read-only here on purpose. The types are eight different editors - a
 * multi-select is not a text box - and half an editing story in a table cell is
 * worse than none: the task dialog already edits all eight properly, and this
 * column exists so somebody can see the value across a list without opening
 * anything.
 */
function customValue(task: Task, field: FieldDefinition): string {
  const stored = (task as Task & { customFields?: Record<string, unknown> }).customFields ?? {};
  const value = stored[field.key];
  if (value === undefined || value === null || value === '') return '';
  if (Array.isArray(value)) return value.join(', ');
  if (typeof value === 'boolean') return value ? '✓' : '';
  return String(value);
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

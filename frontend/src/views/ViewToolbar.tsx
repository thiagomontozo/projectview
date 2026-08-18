import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import * as PopoverPrimitive from '@radix-ui/react-popover';
import clsx from 'clsx';
import styles from './views.module.css';
import overlays from '../ui/overlays.module.css';
import { Button } from '../ui/Button';
import { Input } from '../ui/Field';
import { Avatar } from '../ui/display';
import { Menu, MenuContent, MenuItem, MenuLabel, MenuTrigger } from '../ui/Menu';
import {
  Calendar as CalendarIcon,
  Chart,
  ChevronDown,
  Dashboard,
  Layers,
  Search,
  Table as TableIcon,
  Timeline as TimelineIcon,
  Users
} from '../ui/icons';
import type { GroupBy, ViewKind, ViewState } from './useViewState';
import type { SavedView } from '../types';
import type { ProjectStatusColumn, PublicUser } from '../types';

const VIEWS: Array<{ kind: ViewKind; labelKey: string; Icon: typeof Dashboard }> = [
  { kind: 'board', labelKey: 'views.board', Icon: Dashboard },
  { kind: 'list', labelKey: 'views.list', Icon: Layers },
  { kind: 'table', labelKey: 'views.table', Icon: TableIcon },
  { kind: 'calendar', labelKey: 'views.calendar', Icon: CalendarIcon },
  { kind: 'timeline', labelKey: 'views.timeline', Icon: TimelineIcon },
  { kind: 'workload', labelKey: 'views.workload', Icon: Users }
];

const PRIORITIES = ['urgent', 'high', 'medium', 'low'] as const;

interface Props {
  state: ViewState;
  statuses: ProjectStatusColumn[];
  members: PublicUser[];
  activeFilterCount: number;
  onKindChange: (kind: ViewKind) => void;
  onGroupByChange: (groupBy: GroupBy) => void;
  onFiltersChange: (update: Partial<ViewState['filters']>) => void;
  onClearFilters: () => void;
  /**
   * Views somebody saved on this project.
   *
   * The arrangement always existed; it just lived in a React state that a
   * reload discarded, so everybody rebuilt the same filter every morning and no
   * two people could agree on what "the board" meant. These are shared - a
   * saved view belongs to the project, not to whoever pressed the button.
   */
  savedViews?: SavedView[];
  onApplyView?: (view: SavedView) => void;
  onSaveView?: (name: string) => void;
  onDeleteView?: (id: string) => void;
}

export function ViewToolbar({
  state,
  statuses,
  members,
  activeFilterCount,
  onKindChange,
  onGroupByChange,
  onFiltersChange,
  onClearFilters,
  savedViews = [],
  onApplyView,
  onSaveView,
  onDeleteView
}: Props) {
  const { t } = useTranslation();
  const [naming, setNaming] = useState(false);
  const [draftName, setDraftName] = useState('');

  const toggle = (list: string[], value: string) =>
    list.includes(value) ? list.filter((item) => item !== value) : [...list, value];

  // Grouping only means something where rows are stacked; a calendar groups by
  // date and a timeline by row, so offering it there would be a dead control.
  const groupingApplies = state.kind === 'list' || state.kind === 'table';

  return (
    <div className={styles.toolbar}>
      <div className={styles.switcher} role="tablist" aria-label={t('views.label')}>
        {VIEWS.map(({ kind, labelKey, Icon }) => (
          <button
            key={kind}
            type="button"
            role="tab"
            aria-selected={state.kind === kind}
            className={clsx(styles.switcherItem, state.kind === kind && styles.switcherItemActive)}
            onClick={() => onKindChange(kind)}
          >
            <Icon size={15} />
            {t(labelKey)}
          </button>
        ))}

        {savedViews.map((view) => (
          <span key={view.id} className={styles.savedView}>
            <button
              type="button"
              role="tab"
              aria-selected={false}
              className={styles.switcherItem}
              onClick={() => onApplyView?.(view)}
            >
              {view.name}
            </button>
            {onDeleteView && (
              <button
                type="button"
                className={styles.savedViewRemove}
                aria-label={t('views.deleteView', { name: view.name })}
                onClick={() => onDeleteView(view.id)}
              >
                ×
              </button>
            )}
          </span>
        ))}

        {onSaveView &&
          (naming ? (
            <form
              className={styles.savedView}
              onSubmit={(event) => {
                event.preventDefault();
                const name = draftName.trim();
                if (!name) return;
                onSaveView(name);
                setDraftName('');
                setNaming(false);
              }}
            >
              <Input
                autoFocus
                value={draftName}
                onChange={(event) => setDraftName(event.target.value)}
                placeholder={t('views.viewName')}
                aria-label={t('views.viewName')}
                style={{ width: 160 }}
                onKeyDown={(event) => event.key === 'Escape' && setNaming(false)}
              />
              <Button type="submit" size="sm" variant="primary">
                {t('common.save')}
              </Button>
            </form>
          ) : (
            <button type="button" className={styles.switcherItem} onClick={() => setNaming(true)}>
              + {t('views.addView')}
            </button>
          ))}
      </div>

      <div className={styles.toolbarSpacer} />

      <Input
        value={state.filters.search}
        onChange={(event) => onFiltersChange({ search: event.target.value })}
        placeholder={t('views.searchTasks')}
        aria-label={t('views.searchTasks')}
        icon={<Search size={15} />}
        style={{ width: 220 }}
      />

      {groupingApplies && (
        <Menu>
          <MenuTrigger asChild>
            <Button variant="secondary" size="sm">
              {t('views.groupBy')}: {t(`views.groupBy_${state.groupBy}`)}
              <ChevronDown size={14} />
            </Button>
          </MenuTrigger>
          <MenuContent>
            <MenuLabel>{t('views.groupBy')}</MenuLabel>
            {(['status', 'assignee', 'priority', 'none'] as GroupBy[]).map((option) => (
              <MenuItem key={option} onSelect={() => onGroupByChange(option)}>
                {t(`views.groupBy_${option}`)}
                {state.groupBy === option && <span className={overlays.menuShortcut}>✓</span>}
              </MenuItem>
            ))}
          </MenuContent>
        </Menu>
      )}

      <PopoverPrimitive.Root>
        <PopoverPrimitive.Trigger asChild>
          <Button variant="secondary" size="sm">
            {t('views.filters')}
            {activeFilterCount > 0 && <span className={styles.filterCount}>{activeFilterCount}</span>}
          </Button>
        </PopoverPrimitive.Trigger>
        <PopoverPrimitive.Portal>
          <PopoverPrimitive.Content className={overlays.menu} align="end" sideOffset={6}>
            <div className={styles.filterPanel}>
              <fieldset className={styles.filterGroup} style={{ border: 'none', padding: 0, margin: 0 }}>
                <legend className={styles.filterLegend}>{t('task.status')}</legend>
                <div className={styles.chipRow}>
                  {statuses.map((status) => (
                    <button
                      key={status.key}
                      type="button"
                      aria-pressed={state.filters.status.includes(status.key)}
                      className={clsx(styles.chip, state.filters.status.includes(status.key) && styles.chipActive)}
                      onClick={() => onFiltersChange({ status: toggle(state.filters.status, status.key) })}
                    >
                      {status.label}
                    </button>
                  ))}
                </div>
              </fieldset>

              <fieldset className={styles.filterGroup} style={{ border: 'none', padding: 0, margin: 0 }}>
                <legend className={styles.filterLegend}>{t('task.priority')}</legend>
                <div className={styles.chipRow}>
                  {PRIORITIES.map((priority) => (
                    <button
                      key={priority}
                      type="button"
                      aria-pressed={state.filters.priority.includes(priority)}
                      className={clsx(styles.chip, state.filters.priority.includes(priority) && styles.chipActive)}
                      onClick={() => onFiltersChange({ priority: toggle(state.filters.priority, priority) })}
                    >
                      {t(`task.priority${priority.charAt(0).toUpperCase()}${priority.slice(1)}`)}
                    </button>
                  ))}
                </div>
              </fieldset>

              {members.length > 0 && (
                <fieldset className={styles.filterGroup} style={{ border: 'none', padding: 0, margin: 0 }}>
                  <legend className={styles.filterLegend}>{t('task.assignees')}</legend>
                  <div className={styles.chipRow}>
                    {members.map((member) => (
                      <button
                        key={member.id}
                        type="button"
                        aria-pressed={state.filters.assignees.includes(member.id)}
                        className={clsx(
                          styles.chip,
                          state.filters.assignees.includes(member.id) && styles.chipActive
                        )}
                        onClick={() => onFiltersChange({ assignees: toggle(state.filters.assignees, member.id) })}
                        style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--space-2)' }}
                      >
                        <Avatar name={member.name} color={member.avatarColor} size={16} />
                        {member.name}
                      </button>
                    ))}
                  </div>
                </fieldset>
              )}

              <div className={styles.chipRow}>
                <button
                  type="button"
                  aria-pressed={state.filters.overdueOnly}
                  className={clsx(styles.chip, state.filters.overdueOnly && styles.chipActive)}
                  onClick={() => onFiltersChange({ overdueOnly: !state.filters.overdueOnly })}
                >
                  {t('views.overdueOnly')}
                </button>
              </div>

              {activeFilterCount > 0 && (
                <Button variant="ghost" size="sm" onClick={onClearFilters}>
                  {t('views.clearFilters')}
                </Button>
              )}
            </div>
          </PopoverPrimitive.Content>
        </PopoverPrimitive.Portal>
      </PopoverPrimitive.Root>
    </div>
  );
}

interface BulkBarProps {
  count: number;
  statuses: ProjectStatusColumn[];
  onStatusChange: (status: string) => void;
  onPriorityChange: (priority: string) => void;
  onClear: () => void;
  busy?: boolean;
}

/** Acts on every selected task at once, from the views that support selection. */
export function BulkActionBar({ count, statuses, onStatusChange, onPriorityChange, onClear, busy }: BulkBarProps) {
  const { t } = useTranslation();
  if (count === 0) return null;

  return (
    <div className={styles.bulkBar} role="region" aria-label={t('views.bulkSelected', { count })}>
      <span className={styles.bulkCount}>{t('views.bulkSelected', { count })}</span>

      <Menu>
        <MenuTrigger asChild>
          <Button variant="secondary" size="sm" loading={busy}>
            {t('views.setStatus')}
            <ChevronDown size={14} />
          </Button>
        </MenuTrigger>
        <MenuContent align="center">
          {statuses.map((status) => (
            <MenuItem key={status.key} onSelect={() => onStatusChange(status.key)}>
              {status.label}
            </MenuItem>
          ))}
        </MenuContent>
      </Menu>

      <Menu>
        <MenuTrigger asChild>
          <Button variant="secondary" size="sm" loading={busy}>
            {t('views.setPriority')}
            <ChevronDown size={14} />
          </Button>
        </MenuTrigger>
        <MenuContent align="center">
          {PRIORITIES.map((priority) => (
            <MenuItem key={priority} onSelect={() => onPriorityChange(priority)}>
              {t(`task.priority${priority.charAt(0).toUpperCase()}${priority.slice(1)}`)}
            </MenuItem>
          ))}
        </MenuContent>
      </Menu>

      <Button variant="ghost" size="sm" onClick={onClear}>
        {t('views.clearSelection')}
      </Button>
    </div>
  );
}

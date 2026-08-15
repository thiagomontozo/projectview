import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import styles from './views.module.css';
import { Avatar, EmptyState } from '../ui/display';
import { Users } from '../ui/icons';
import type { PublicUser, Task } from '../types';

interface Props {
  tasks: Task[];
  members: PublicUser[];
}

/** Hours a person is assumed available per working day. */
const DAILY_CAPACITY = 6;
const WEEKS_AHEAD = 6;
const DAY_MS = 86_400_000;

function startOfWeek(date: Date): Date {
  const copy = new Date(date);
  copy.setHours(0, 0, 0, 0);
  copy.setDate(copy.getDate() - copy.getDay());
  return copy;
}

/**
 * Capacity against allocation, per person, per week.
 *
 * A task's estimate is spread evenly across the days it spans rather than
 * charged to its due date: eighty hours landing on one Friday would show a
 * spike that does not exist, and hide the six weeks of load that do.
 *
 * Unestimated tasks contribute nothing — inventing a default would make the
 * whole view fiction. The count is shown alongside the hours so a column of
 * zeroes with visible work is legible as "nobody estimated these".
 */
export function WorkloadView({ tasks, members }: Props) {
  const { t, i18n } = useTranslation();

  const weeks = useMemo(() => {
    const first = startOfWeek(new Date());
    return Array.from({ length: WEEKS_AHEAD }, (_, i) => new Date(first.getTime() + i * 7 * DAY_MS));
  }, []);

  const load = useMemo(() => {
    // userId -> week index -> { hours, taskCount }
    const map = new Map<string, Array<{ hours: number; count: number }>>();
    const ensure = (userId: string) => {
      let row = map.get(userId);
      if (!row) {
        row = weeks.map(() => ({ hours: 0, count: 0 }));
        map.set(userId, row);
      }
      return row;
    };

    tasks.forEach((task) => {
      if (task.status === 'done' || task.assignees.length === 0) return;

      const start = task.startDate ? new Date(task.startDate) : task.dueDate ? new Date(task.dueDate) : null;
      const end = task.dueDate ? new Date(task.dueDate) : start;
      if (!start || !end) return;

      const spanDays = Math.max(1, Math.round((end.getTime() - start.getTime()) / DAY_MS) + 1);
      // Shared work is split, not double-counted: two people on a 10-hour
      // task carry five hours each.
      const hoursPerDayPerPerson = task.estimateHours / spanDays / task.assignees.length;

      task.assignees.forEach((assignee) => {
        const row = ensure(assignee.id);
        for (let dayOffset = 0; dayOffset < spanDays; dayOffset++) {
          const day = new Date(start.getTime() + dayOffset * DAY_MS);
          const weekIndex = weeks.findIndex(
            (weekStart) => day >= weekStart && day < new Date(weekStart.getTime() + 7 * DAY_MS)
          );
          if (weekIndex === -1) continue;
          row[weekIndex].hours += hoursPerDayPerPerson;
          if (dayOffset === 0) row[weekIndex].count += 1;
        }
      });
    });

    return map;
  }, [tasks, weeks]);

  const withLoad = members.filter((member) => load.has(member.id));

  if (withLoad.length === 0) {
    return <EmptyState icon={<Users size={22} />} title={t('resources.empty')} body={t('resources.emptyBody')} />;
  }

  const weeklyCapacity = DAILY_CAPACITY * 5;
  const weekFormatter = new Intl.DateTimeFormat(i18n.language, { day: '2-digit', month: 'short' });

  const band = (hours: number) => {
    if (hours > weeklyCapacity) return styles.workloadOver;
    if (hours >= weeklyCapacity * 0.75) return styles.workloadFull;
    if (hours > 0) return styles.workloadLight;
    return undefined;
  };

  return (
    <div>
      <div className={styles.timeline}>
        <div className={styles.workloadRow} style={{ background: 'var(--surface-sunken)' }}>
          <div className={styles.workloadPerson}>
            <span className={styles.groupLabel}>{t('resources.person')}</span>
          </div>
          <div className={styles.workloadTrack}>
            {weeks.map((week) => (
              <div key={week.toISOString()} className={styles.workloadCell} style={{ background: 'transparent' }}>
                {weekFormatter.format(week)}
              </div>
            ))}
          </div>
        </div>

        {withLoad.map((member) => {
          const row = load.get(member.id)!;
          return (
            <div key={member.id} className={styles.workloadRow}>
              <div className={styles.workloadPerson}>
                <Avatar name={member.name} color={member.avatarColor} size={26} />
                <span style={{ fontSize: 'var(--text-sm)' }}>{member.name}</span>
              </div>
              <div className={styles.workloadTrack}>
                {row.map((cell, index) => (
                  <div
                    key={index}
                    className={clsx(styles.workloadCell, band(cell.hours))}
                    title={t('views.workloadCell', {
                      hours: Math.round(cell.hours),
                      capacity: weeklyCapacity,
                      count: cell.count
                    })}
                  >
                    {cell.hours > 0 ? `${Math.round(cell.hours)}h` : '—'}
                  </div>
                ))}
              </div>
            </div>
          );
        })}
      </div>

      <div className={styles.legend}>
        <span className={styles.legendItem}>
          <span className={styles.legendSwatch} style={{ background: 'var(--success-subtle)' }} />
          {t('views.loadLight')}
        </span>
        <span className={styles.legendItem}>
          <span className={styles.legendSwatch} style={{ background: 'var(--warning-subtle)' }} />
          {t('views.loadFull')}
        </span>
        <span className={styles.legendItem}>
          <span className={styles.legendSwatch} style={{ background: 'var(--danger-subtle)' }} />
          {t('views.loadOver', { capacity: weeklyCapacity })}
        </span>
      </div>
    </div>
  );
}

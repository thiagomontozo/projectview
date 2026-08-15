import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import styles from './views.module.css';
import { Button } from '../ui/Button';
import { ChevronLeft, ChevronRight } from '../ui/icons';
import { isOverdue } from '../lib/format';
import type { Task } from '../types';

interface Props {
  tasks: Task[];
  onOpenTask: (task: Task) => void;
}

const MAX_PER_DAY = 3;

/** Local-midnight key, so a task never lands on the wrong day through UTC. */
function dayKey(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
}

export function CalendarView({ tasks, onOpenTask }: Props) {
  const { t, i18n } = useTranslation();
  const [cursor, setCursor] = useState(() => new Date());

  const { weeks, monthLabel } = useMemo(() => {
    const year = cursor.getFullYear();
    const month = cursor.getMonth();

    const firstOfMonth = new Date(year, month, 1);
    // Grid starts on the Sunday on or before the 1st, so weeks line up.
    const start = new Date(firstOfMonth);
    start.setDate(start.getDate() - start.getDay());

    const days: Date[] = [];
    for (let i = 0; i < 42; i++) {
      const day = new Date(start);
      day.setDate(start.getDate() + i);
      days.push(day);
    }

    const grouped: Date[][] = [];
    for (let i = 0; i < days.length; i += 7) grouped.push(days.slice(i, i + 7));

    return {
      weeks: grouped,
      monthLabel: new Intl.DateTimeFormat(i18n.language, { month: 'long', year: 'numeric' }).format(firstOfMonth)
    };
  }, [cursor, i18n.language]);

  // Tasks bucketed by their due date, which is what a calendar of work shows.
  const byDay = useMemo(() => {
    const map = new Map<string, Task[]>();
    tasks.forEach((task) => {
      if (!task.dueDate) return;
      const key = dayKey(new Date(task.dueDate));
      const list = map.get(key);
      if (list) list.push(task);
      else map.set(key, [task]);
    });
    return map;
  }, [tasks]);

  const weekdayNames = useMemo(() => {
    const formatter = new Intl.DateTimeFormat(i18n.language, { weekday: 'short' });
    // 2024-01-07 is a Sunday; walking forward gives the week in order.
    return Array.from({ length: 7 }, (_, i) => formatter.format(new Date(2024, 0, 7 + i)));
  }, [i18n.language]);

  const todayKey = dayKey(new Date());
  const currentMonth = cursor.getMonth();

  const shiftMonth = (delta: number) =>
    setCursor((current) => new Date(current.getFullYear(), current.getMonth() + delta, 1));

  return (
    <div>
      <div className={styles.calendarHeader}>
        <Button variant="secondary" size="sm" iconOnly aria-label={t('views.previousMonth')} onClick={() => shiftMonth(-1)}>
          <ChevronLeft size={16} />
        </Button>
        <Button variant="secondary" size="sm" iconOnly aria-label={t('views.nextMonth')} onClick={() => shiftMonth(1)}>
          <ChevronRight size={16} />
        </Button>
        <span className={styles.calendarMonth}>{monthLabel}</span>
        <Button variant="ghost" size="sm" onClick={() => setCursor(new Date())}>
          {t('views.today')}
        </Button>
      </div>

      <div className={styles.calendarGrid}>
        {weekdayNames.map((name) => (
          <div key={name} className={styles.calendarWeekday}>
            {name}
          </div>
        ))}

        {weeks.flat().map((day) => {
          const key = dayKey(day);
          const dayTasks = byDay.get(key) ?? [];
          const outside = day.getMonth() !== currentMonth;

          return (
            <div
              key={key}
              className={clsx(
                styles.calendarDay,
                outside && styles.calendarDayOutside,
                key === todayKey && styles.calendarDayToday
              )}
            >
              <span className={styles.calendarDayNumber}>{day.getDate()}</span>

              {dayTasks.slice(0, MAX_PER_DAY).map((task) => (
                <button
                  key={task.id}
                  type="button"
                  className={clsx(
                    styles.calendarTask,
                    task.status === 'done' && styles.calendarTaskDone,
                    isOverdue(task.dueDate, task.status) && styles.calendarTaskOverdue
                  )}
                  onClick={() => onOpenTask(task)}
                  title={task.title}
                >
                  {task.title}
                </button>
              ))}

              {dayTasks.length > MAX_PER_DAY && (
                <span className={styles.calendarMore}>
                  {t('views.moreTasks', { count: dayTasks.length - MAX_PER_DAY })}
                </span>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

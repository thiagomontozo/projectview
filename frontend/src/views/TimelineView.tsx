import { useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import styles from './views.module.css';
import { EmptyState } from '../ui/display';
import { Timeline as TimelineIcon } from '../ui/icons';
import { isOverdue } from '../lib/format';
import type { Task } from '../types';

interface Props {
  tasks: Task[];
  onOpenTask: (task: Task) => void;
  onReschedule: (taskId: string, startDate: string, dueDate: string) => void;
}

const DAY_WIDTH = 30;
const DAY_MS = 86_400_000;

function startOfDay(date: Date): Date {
  const copy = new Date(date);
  copy.setHours(0, 0, 0, 0);
  return copy;
}

function addDays(date: Date, days: number): Date {
  const copy = new Date(date);
  copy.setDate(copy.getDate() + days);
  return copy;
}

/**
 * Gantt-style timeline.
 *
 * A bar spans a task's start and due dates; a task with neither is absent,
 * because placing an unscheduled task somewhere on a time axis would invent
 * information. Tasks whose two dates fall on the same day render as a
 * milestone diamond rather than a one-pixel bar.
 *
 * Dragging a bar moves the whole task, preserving its duration, and commits
 * once on release — a request per pixel would be unusable.
 */
export function TimelineView({ tasks, onOpenTask, onReschedule }: Props) {
  const { t, i18n } = useTranslation();
  const [drag, setDrag] = useState<{ taskId: string; offsetDays: number } | null>(null);
  const trackRef = useRef<HTMLDivElement>(null);

  const scheduled = useMemo(
    () => tasks.filter((task) => task.startDate || task.dueDate),
    [tasks]
  );

  const { rangeStart, dayCount, days } = useMemo(() => {
    if (scheduled.length === 0) {
      const today = startOfDay(new Date());
      return { rangeStart: today, dayCount: 30, days: [] as Date[] };
    }

    let min = Infinity;
    let max = -Infinity;
    scheduled.forEach((task) => {
      const start = startOfDay(new Date(task.startDate ?? task.dueDate!)).getTime();
      const end = startOfDay(new Date(task.dueDate ?? task.startDate!)).getTime();
      min = Math.min(min, start);
      max = Math.max(max, end);
    });

    // Pad the range so the first and last bars are not flush against the edge.
    const start = addDays(new Date(min), -3);
    const end = addDays(new Date(max), 3);
    const count = Math.max(14, Math.round((end.getTime() - start.getTime()) / DAY_MS) + 1);

    return {
      rangeStart: start,
      dayCount: count,
      days: Array.from({ length: count }, (_, i) => addDays(start, i))
    };
  }, [scheduled]);

  if (scheduled.length === 0) {
    return (
      <EmptyState icon={<TimelineIcon size={22} />} title={t('views.timelineEmpty')} body={t('views.timelineEmptyBody')} />
    );
  }

  const dayIndex = (date: Date) => Math.round((startOfDay(date).getTime() - rangeStart.getTime()) / DAY_MS);
  const todayOffset = dayIndex(new Date()) * DAY_WIDTH;

  const monthFormatter = new Intl.DateTimeFormat(i18n.language, { month: 'short', year: 'numeric' });

  // One label per month, sized to the days it covers.
  const monthSpans: Array<{ label: string; span: number }> = [];
  days.forEach((day) => {
    const label = monthFormatter.format(day);
    const last = monthSpans.at(-1);
    if (last && last.label === label) last.span += 1;
    else monthSpans.push({ label, span: 1 });
  });

  function handlePointerDown(event: React.PointerEvent, task: Task) {
    if (!task.startDate && !task.dueDate) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    setDrag({ taskId: task.id, offsetDays: 0 });

    const originX = event.clientX;

    const onMove = (moveEvent: PointerEvent) => {
      const deltaDays = Math.round((moveEvent.clientX - originX) / DAY_WIDTH);
      setDrag({ taskId: task.id, offsetDays: deltaDays });
    };

    const onUp = (upEvent: PointerEvent) => {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);

      const deltaDays = Math.round((upEvent.clientX - originX) / DAY_WIDTH);
      setDrag(null);
      if (deltaDays === 0) return;

      const start = new Date(task.startDate ?? task.dueDate!);
      const end = new Date(task.dueDate ?? task.startDate!);
      onReschedule(
        task.id,
        addDays(start, deltaDays).toISOString(),
        addDays(end, deltaDays).toISOString()
      );
    };

    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
  }

  return (
    <div className={styles.timeline}>
      <div className={styles.timelineScroller}>
        <div className={styles.timelineGrid}>
          <div className={styles.timelineHeader}>
            <div className={styles.timelineLabelSpacer} />
            {monthSpans.map((month) => (
              <div
                key={month.label}
                className={styles.timelineMonthLabel}
                style={{ width: month.span * DAY_WIDTH }}
              >
                {month.label}
              </div>
            ))}
          </div>

          <div className={styles.timelineHeader}>
            <div className={styles.timelineLabelSpacer} />
            {days.map((day) => {
              const weekend = day.getDay() === 0 || day.getDay() === 6;
              return (
                <div
                  key={day.toISOString()}
                  className={clsx(styles.timelineDay, weekend && styles.timelineDayWeekend)}
                  style={{ width: DAY_WIDTH }}
                >
                  {day.getDate()}
                </div>
              );
            })}
          </div>

          <div style={{ position: 'relative' }} ref={trackRef}>
            {todayOffset >= 0 && todayOffset <= dayCount * DAY_WIDTH && (
              <div className={styles.timelineToday} style={{ left: 220 + todayOffset }} aria-hidden="true" />
            )}

            {scheduled.map((task) => {
              const start = new Date(task.startDate ?? task.dueDate!);
              const end = new Date(task.dueDate ?? task.startDate!);
              const dragOffset = drag?.taskId === task.id ? drag.offsetDays : 0;

              const startIndex = dayIndex(start) + dragOffset;
              const endIndex = dayIndex(end) + dragOffset;
              const spanDays = Math.max(1, endIndex - startIndex + 1);
              const isMilestone = endIndex === startIndex;

              return (
                <div key={task.id} className={styles.timelineRow}>
                  <div className={styles.timelineLabel} title={task.title}>
                    {task.title}
                  </div>
                  <div className={styles.timelineTrack} style={{ width: dayCount * DAY_WIDTH }}>
                    {isMilestone ? (
                      <button
                        type="button"
                        className={styles.timelineMilestone}
                        style={{ left: startIndex * DAY_WIDTH + DAY_WIDTH / 2 - 8 }}
                        onPointerDown={(event) => handlePointerDown(event, task)}
                        onClick={() => onOpenTask(task)}
                        aria-label={t('views.milestone', { title: task.title })}
                      />
                    ) : (
                      <button
                        type="button"
                        className={clsx(
                          styles.timelineBar,
                          task.status === 'done' && styles.timelineBarDone,
                          isOverdue(task.dueDate, task.status) && styles.timelineBarOverdue,
                          drag?.taskId === task.id && styles.timelineBarDragging
                        )}
                        style={{ left: startIndex * DAY_WIDTH, width: spanDays * DAY_WIDTH - 4 }}
                        onPointerDown={(event) => handlePointerDown(event, task)}
                        onClick={() => !drag && onOpenTask(task)}
                        aria-label={t('views.taskBar', { title: task.title })}
                      >
                        {task.title}
                      </button>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}

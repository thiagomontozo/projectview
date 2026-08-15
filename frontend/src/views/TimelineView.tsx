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
  /** Edges to draw. taskId waits on dependsOn. */
  dependencies?: Array<{ taskId: string; dependsOn: string }>;
  /** Ids on the chain where any slip moves the project's end date. */
  criticalPath?: string[];
}

const ROW_HEIGHT = 38;
const LABEL_WIDTH = 220;

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
export function TimelineView({
  tasks,
  onOpenTask,
  onReschedule,
  dependencies = [],
  criticalPath = []
}: Props) {
  const { t, i18n } = useTranslation();
  const [drag, setDrag] = useState<{ taskId: string; offsetDays: number } | null>(null);
  const trackRef = useRef<HTMLDivElement>(null);

  const criticalSet = useMemo(() => new Set(criticalPath), [criticalPath]);

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
              <div className={styles.timelineToday} style={{ left: LABEL_WIDTH + todayOffset }} aria-hidden="true" />
            )}

            <DependencyArrows
              tasks={scheduled}
              dependencies={dependencies}
              criticalPath={criticalPath}
              dayIndex={dayIndex}
              dayCount={dayCount}
            />

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
                          criticalSet.has(task.id) && styles.timelineBarCritical,
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

      {criticalPath.length > 0 && (
        <div className={styles.legend} style={{ padding: 'var(--space-3)' }}>
          <span className={styles.legendItem}>
            <span className={styles.legendSwatch} style={{ background: 'var(--danger)' }} />
            {t('views.criticalPath')}
          </span>
        </div>
      )}
    </div>
  );
}

/**
 * Dependency arrows, drawn as one SVG overlay rather than per-row elements.
 *
 * A single absolutely-positioned layer keeps the arrows from being clipped by
 * each row's overflow, and lets a path run from any row to any other. The
 * layer ignores pointer events so the bars underneath stay draggable.
 */
function DependencyArrows({
  tasks,
  dependencies,
  criticalPath,
  dayIndex,
  dayCount
}: {
  tasks: Task[];
  dependencies: Array<{ taskId: string; dependsOn: string }>;
  criticalPath: string[];
  dayIndex: (date: Date) => number;
  dayCount: number;
}) {
  const rowOf = useMemo(() => {
    const map = new Map<string, number>();
    tasks.forEach((task, index) => map.set(task.id, index));
    return map;
  }, [tasks]);

  const taskById = useMemo(() => {
    const map = new Map<string, Task>();
    tasks.forEach((task) => map.set(task.id, task));
    return map;
  }, [tasks]);

  const criticalEdges = useMemo(() => {
    // An edge is critical only when it joins two consecutive tasks on the
    // path; two critical tasks connected by a side branch are not.
    const set = new Set<string>();
    for (let i = 0; i < criticalPath.length - 1; i++) {
      set.add(`${criticalPath[i + 1]}<-${criticalPath[i]}`);
    }
    return set;
  }, [criticalPath]);

  const edges = dependencies
    .map((dependency) => {
      const from = taskById.get(dependency.dependsOn);
      const to = taskById.get(dependency.taskId);
      const fromRow = rowOf.get(dependency.dependsOn);
      const toRow = rowOf.get(dependency.taskId);
      // Either end may be unscheduled and therefore absent from the chart.
      if (!from || !to || fromRow === undefined || toRow === undefined) return null;

      const fromEnd = dayIndex(new Date(from.dueDate ?? from.startDate!)) + 1;
      const toStart = dayIndex(new Date(to.startDate ?? to.dueDate!));

      return {
        key: `${dependency.taskId}<-${dependency.dependsOn}`,
        x1: LABEL_WIDTH + fromEnd * DAY_WIDTH,
        y1: fromRow * ROW_HEIGHT + ROW_HEIGHT / 2,
        x2: LABEL_WIDTH + toStart * DAY_WIDTH,
        y2: toRow * ROW_HEIGHT + ROW_HEIGHT / 2,
        critical: criticalEdges.has(`${dependency.taskId}<-${dependency.dependsOn}`)
      };
    })
    .filter((edge): edge is NonNullable<typeof edge> => edge !== null);

  if (edges.length === 0) return null;

  return (
    <svg
      className={styles.arrowLayer}
      width={LABEL_WIDTH + dayCount * DAY_WIDTH}
      height={tasks.length * ROW_HEIGHT}
      aria-hidden="true"
    >
      <defs>
        <marker id="arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
          <path d="M0,0 L8,4 L0,8 z" fill="var(--text-muted)" />
        </marker>
        <marker id="arrow-critical" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
          <path d="M0,0 L8,4 L0,8 z" fill="var(--danger)" />
        </marker>
      </defs>

      {edges.map((edge) => {
        // Elbow route: out of the predecessor, across, then into the
        // successor. A straight diagonal would cross unrelated bars.
        const midX = Math.max(edge.x1 + 8, edge.x2 - 12);
        const path = `M ${edge.x1} ${edge.y1} H ${midX} V ${edge.y2} H ${edge.x2}`;
        return (
          <path
            key={edge.key}
            d={path}
            className={clsx(styles.arrow, edge.critical && styles.arrowCritical)}
            markerEnd={edge.critical ? 'url(#arrow-critical)' : 'url(#arrow)'}
          />
        );
      })}
    </svg>
  );
}

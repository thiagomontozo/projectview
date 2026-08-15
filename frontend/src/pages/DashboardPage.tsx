import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { DndContext, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core';
import { SortableContext, arrayMove, rectSortingStrategy, useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import { PageHeader } from '../app/AppShell';
import { Card, ErrorState } from '../ui/display';
import cardStyles from '../ui/display.module.css';
import { Button } from '../ui/Button';
import { Skeleton } from '../ui/Skeleton';
import StatusPie from '../components/charts/StatusPie';
import ProjectProgress from '../components/charts/ProjectProgress';
import WorkloadBar from '../components/charts/WorkloadBar';
import CompletionTrend from '../components/charts/CompletionTrend';
import {
  useCompletionTrend,
  useOverview,
  useProjectProgress,
  useStatusBreakdown,
  useWorkloadChart
} from '../lib/queries';
import styles from './pages.module.css';

const LAYOUT_KEY = 'pv_dashboard_layout';

const WIDGET_IDS = [
  'kpi-active-projects',
  'kpi-total-projects',
  'kpi-total-tasks',
  'kpi-done-tasks',
  'kpi-overdue-tasks',
  'chart-status',
  'chart-progress',
  'chart-workload',
  'chart-trend'
] as const;

type WidgetId = (typeof WIDGET_IDS)[number];

function loadLayout(): WidgetId[] {
  const fallback = [...WIDGET_IDS];
  try {
    const raw = localStorage.getItem(LAYOUT_KEY);
    if (!raw) return fallback;
    const saved = JSON.parse(raw) as string[];
    // Keep only ids we still know about, then append any newly added widget,
    // so an older saved layout never hides a card.
    const known = saved.filter((id): id is WidgetId => (WIDGET_IDS as readonly string[]).includes(id));
    return [...known, ...fallback.filter((id) => !known.includes(id))];
  } catch {
    return fallback;
  }
}

function SortableWidget({
  id,
  label,
  wide,
  children
}: {
  id: WidgetId;
  label: string;
  wide?: boolean;
  children: ReactNode;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id });

  // A plain section rather than <Card>: dnd-kit needs the ref on the DOM node
  // that carries the transform, and threading a ref through the Card wrapper
  // would buy nothing but indirection.
  return (
    <section
      ref={setNodeRef}
      className={clsx(cardStyles.card, styles.widget, wide ? styles.widgetChart : styles.widgetKpi)}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
        zIndex: isDragging ? 5 : undefined
      }}
      aria-label={label}
    >
      <button type="button" className={styles.dragHandle} aria-label={label} {...attributes} {...listeners}>
        <span aria-hidden="true">⠿</span>
      </button>
      {children}
    </section>
  );
}

function Kpi({ label, value, tone }: { label: string; value: number | undefined; tone?: string }) {
  return (
    <>
      <div className={styles.kpiLabel}>{label}</div>
      <div className={styles.kpiValue} style={{ color: tone }}>
        {value === undefined ? <Skeleton width={64} height={34} /> : value}
      </div>
    </>
  );
}

export default function DashboardPage() {
  const { t } = useTranslation();
  const [layout, setLayout] = useState<WidgetId[]>(loadLayout);

  const overview = useOverview();
  const statusBreakdown = useStatusBreakdown();
  const projectProgress = useProjectProgress();
  const workloadChart = useWorkloadChart();
  const completionTrend = useCompletionTrend();

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));

  useEffect(() => {
    try {
      localStorage.setItem(LAYOUT_KEY, JSON.stringify(layout));
    } catch {
      /* storage unavailable; the layout lasts for this session */
    }
  }, [layout]);

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    setLayout((current) => {
      const from = current.indexOf(active.id as WidgetId);
      const to = current.indexOf(over.id as WidgetId);
      if (from === -1 || to === -1) return current;
      return arrayMove(current, from, to);
    });
  }

  const widgets = useMemo<Record<WidgetId, { label: string; wide?: boolean; content: ReactNode }>>(
    () => ({
      'kpi-active-projects': {
        label: t('dashboard.activeProjects'),
        content: <Kpi label={t('dashboard.activeProjects')} value={overview.data?.activeProjects} />
      },
      'kpi-total-projects': {
        label: t('dashboard.totalProjects'),
        content: <Kpi label={t('dashboard.totalProjects')} value={overview.data?.totalProjects} />
      },
      'kpi-total-tasks': {
        label: t('dashboard.totalTasks'),
        content: <Kpi label={t('dashboard.totalTasks')} value={overview.data?.totalTasks} />
      },
      'kpi-done-tasks': {
        label: t('dashboard.doneTasks'),
        content: (
          <Kpi label={t('dashboard.doneTasks')} value={overview.data?.doneTasks} tone="var(--success)" />
        )
      },
      'kpi-overdue-tasks': {
        label: t('dashboard.overdueTasks'),
        content: (
          <Kpi label={t('dashboard.overdueTasks')} value={overview.data?.overdueTasks} tone="var(--danger)" />
        )
      },
      'chart-status': {
        label: t('dashboard.tasksByStatus'),
        wide: true,
        content: (
          <>
            <h2 className={styles.chartTitle}>{t('dashboard.tasksByStatus')}</h2>
            {statusBreakdown.isLoading ? (
              <Skeleton height={260} />
            ) : (
              <StatusPie data={statusBreakdown.data ?? []} />
            )}
          </>
        )
      },
      'chart-progress': {
        label: t('dashboard.projectProgress'),
        wide: true,
        content: (
          <>
            <h2 className={styles.chartTitle}>{t('dashboard.projectProgress')}</h2>
            {projectProgress.isLoading ? (
              <Skeleton height={260} />
            ) : (
              <ProjectProgress data={projectProgress.data ?? []} />
            )}
          </>
        )
      },
      'chart-workload': {
        label: t('dashboard.workload'),
        wide: true,
        content: (
          <>
            <h2 className={styles.chartTitle}>{t('dashboard.workload')}</h2>
            {workloadChart.isLoading ? <Skeleton height={260} /> : <WorkloadBar data={workloadChart.data ?? []} />}
          </>
        )
      },
      'chart-trend': {
        label: t('dashboard.completions'),
        wide: true,
        content: (
          <>
            <h2 className={styles.chartTitle}>{t('dashboard.completions')}</h2>
            {completionTrend.isLoading ? (
              <Skeleton height={260} />
            ) : (
              <CompletionTrend data={completionTrend.data ?? []} />
            )}
          </>
        )
      }
    }),
    [t, overview.data, statusBreakdown, projectProgress, workloadChart, completionTrend]
  );

  if (overview.isError) {
    return (
      <>
        <PageHeader title={t('dashboard.title')} />
        <Card>
          <ErrorState
            title={t('errors.loadFailed')}
            body={t('errors.genericBody')}
            onRetry={() => void overview.refetch()}
            retryLabel={t('common.retry')}
          />
        </Card>
      </>
    );
  }

  return (
    <>
      <PageHeader
        title={t('dashboard.title')}
        description={t('dashboard.hint')}
        actions={
          <Button variant="secondary" size="sm" onClick={() => setLayout([...WIDGET_IDS])}>
            {t('dashboard.resetLayout')}
          </Button>
        }
      />

      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <SortableContext items={layout} strategy={rectSortingStrategy}>
          <div className={styles.dashGrid}>
            {layout.map((id) => (
              <SortableWidget
                key={id}
                id={id}
                wide={widgets[id].wide}
                label={t('dashboard.moveCard', { name: widgets[id].label })}
              >
                {widgets[id].content}
              </SortableWidget>
            ))}
          </div>
        </SortableContext>
      </DndContext>
    </>
  );
}

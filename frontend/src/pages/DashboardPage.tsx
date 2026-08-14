import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { DndContext, PointerSensor, useSensor, useSensors, closestCenter, type DragEndEvent } from '@dnd-kit/core';
import { SortableContext, rectSortingStrategy, arrayMove, useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import api from '../api/client';
import StatusPie from '../components/charts/StatusPie.tsx';
import ProjectProgress from '../components/charts/ProjectProgress.tsx';
import WorkloadBar from '../components/charts/WorkloadBar.tsx';
import CompletionTrend from '../components/charts/CompletionTrend.tsx';
import { status as statusColors } from '../styles/theme.ts';
import type {
  CompletionTrendRow,
  DashboardOverview,
  ProjectProgressRow,
  StatusBreakdownRow,
  WorkloadChartRow
} from '../types';

const LAYOUT_KEY = 'pv_dashboard_layout';

/**
 * Every dashboard card is a widget with a stable id. The user's preferred
 * order is persisted in localStorage, so a card moved on the dashboard stays
 * where it was put across reloads.
 */
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
    // Keep only ids we still know about, then append any newly added widget
    // so an older saved layout never hides a card.
    const known = saved.filter((id): id is WidgetId => (WIDGET_IDS as readonly string[]).includes(id));
    return [...known, ...fallback.filter((id) => !known.includes(id))];
  } catch {
    return fallback;
  }
}

function SortableWidget({ id, wide, children }: { id: WidgetId; wide?: boolean; children: ReactNode }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id });

  return (
    <div
      ref={setNodeRef}
      className={`card dash-widget${wide ? ' widget-wide' : ''}`}
      style={{
        gridColumn: `span ${wide ? 3 : 2}`,
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.55 : 1,
        zIndex: isDragging ? 5 : undefined
      }}
    >
      <button className="drag-handle" title="Arraste para reposicionar o card" aria-label="Mover card" {...attributes} {...listeners}>
        ⠿
      </button>
      {children}
    </div>
  );
}

function Kpi({ label, value, color }: { label: string; value: string | number; color?: string }) {
  return (
    <>
      <div className="kpi-label">{label}</div>
      <div className="value" style={{ color }}>
        {value}
      </div>
    </>
  );
}

export default function DashboardPage() {
  const [overview, setOverview] = useState<DashboardOverview | null>(null);
  const [statusBreakdown, setStatusBreakdown] = useState<StatusBreakdownRow[]>([]);
  const [progress, setProgress] = useState<ProjectProgressRow[]>([]);
  const [workload, setWorkload] = useState<WorkloadChartRow[]>([]);
  const [trend, setTrend] = useState<CompletionTrendRow[]>([]);
  const [layout, setLayout] = useState<WidgetId[]>(loadLayout);

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));

  useEffect(() => {
    api.get<DashboardOverview>('/dashboard/overview').then((res) => setOverview(res.data));
    api.get<StatusBreakdownRow[]>('/dashboard/status-breakdown').then((res) => setStatusBreakdown(res.data));
    api.get<ProjectProgressRow[]>('/dashboard/project-progress').then((res) => setProgress(res.data));
    api.get<WorkloadChartRow[]>('/dashboard/workload-chart').then((res) => setWorkload(res.data));
    api.get<CompletionTrendRow[]>('/dashboard/completion-trend').then((res) => setTrend(res.data));
  }, []);

  useEffect(() => {
    localStorage.setItem(LAYOUT_KEY, JSON.stringify(layout));
  }, [layout]);

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    setLayout((prev) => {
      const from = prev.indexOf(active.id as WidgetId);
      const to = prev.indexOf(over.id as WidgetId);
      if (from === -1 || to === -1) return prev;
      return arrayMove(prev, from, to);
    });
  }

  const widgets = useMemo<Record<WidgetId, { wide?: boolean; content: ReactNode }>>(
    () => ({
      'kpi-active-projects': { content: <Kpi label="Projetos ativos" value={overview?.activeProjects ?? '—'} /> },
      'kpi-total-projects': { content: <Kpi label="Total de projetos" value={overview?.totalProjects ?? '—'} /> },
      'kpi-total-tasks': { content: <Kpi label="Total de tarefas" value={overview?.totalTasks ?? '—'} /> },
      'kpi-done-tasks': { content: <Kpi label="Concluídas" value={overview?.doneTasks ?? '—'} color={statusColors.good} /> },
      'kpi-overdue-tasks': {
        content: <Kpi label="Atrasadas" value={overview?.overdueTasks ?? '—'} color={statusColors.critical} />
      },
      'chart-status': {
        wide: true,
        content: (
          <>
            <h3>Tarefas por status</h3>
            <StatusPie data={statusBreakdown} />
          </>
        )
      },
      'chart-progress': {
        wide: true,
        content: (
          <>
            <h3>Progresso por projeto</h3>
            <ProjectProgress data={progress} />
          </>
        )
      },
      'chart-workload': {
        wide: true,
        content: (
          <>
            <h3>Carga por recurso</h3>
            <WorkloadBar data={workload} />
          </>
        )
      },
      'chart-trend': {
        wide: true,
        content: (
          <>
            <h3>Conclusões (30 dias)</h3>
            <CompletionTrend data={trend} />
          </>
        )
      }
    }),
    [overview, statusBreakdown, progress, workload, trend]
  );

  return (
    <div>
      <div className="page-header">
        <div>
          <h1>Dashboard</h1>
          <span style={{ fontSize: 13, color: 'var(--text-secondary)' }}>
            Arraste o ⠿ de cada card para reorganizar o painel.
          </span>
        </div>
        <button className="btn btn-sm" onClick={() => setLayout([...WIDGET_IDS])}>
          Restaurar layout
        </button>
      </div>

      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <SortableContext items={layout} strategy={rectSortingStrategy}>
          <div className="dash-grid">
            {layout.map((id) => (
              <SortableWidget key={id} id={id} wide={widgets[id].wide}>
                {widgets[id].content}
              </SortableWidget>
            ))}
          </div>
        </SortableContext>
      </DndContext>
    </div>
  );
}

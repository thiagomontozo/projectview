import { useEffect, useState } from 'react';
import api from '../api/client';
import StatusPie from '../components/charts/StatusPie.tsx';
import WorkloadBar from '../components/charts/WorkloadBar.tsx';
import CompletionTrend from '../components/charts/CompletionTrend.tsx';
import ProjectProgress from '../components/charts/ProjectProgress.tsx';
import type { CompletionTrendRow, ProjectProgressRow, StatusBreakdownRow, WorkloadChartRow } from '../types';

export default function ReportsPage() {
  const [statusBreakdown, setStatusBreakdown] = useState<StatusBreakdownRow[]>([]);
  const [workload, setWorkload] = useState<WorkloadChartRow[]>([]);
  const [trend, setTrend] = useState<CompletionTrendRow[]>([]);
  const [progress, setProgress] = useState<ProjectProgressRow[]>([]);

  useEffect(() => {
    api.get<StatusBreakdownRow[]>('/dashboard/status-breakdown').then((res) => setStatusBreakdown(res.data));
    api.get<WorkloadChartRow[]>('/dashboard/workload-chart').then((res) => setWorkload(res.data));
    api.get<CompletionTrendRow[]>('/dashboard/completion-trend').then((res) => setTrend(res.data));
    api.get<ProjectProgressRow[]>('/dashboard/project-progress').then((res) => setProgress(res.data));
  }, []);

  return (
    <div>
      <div className="page-header">
        <h1>Relatórios</h1>
      </div>

      <div className="grid-2">
        <div className="card chart-card">
          <h3>Distribuição de tarefas por status</h3>
          <StatusPie data={statusBreakdown} />
        </div>
        <div className="card chart-card">
          <h3>Carga de trabalho por recurso (tarefas abertas)</h3>
          <WorkloadBar data={workload} />
        </div>
      </div>

      <div className="grid-2">
        <div className="card chart-card">
          <h3>Tarefas concluídas (últimos 30 dias)</h3>
          <CompletionTrend data={trend} />
        </div>
        <div className="card chart-card">
          <h3>Progresso por projeto</h3>
          <ProjectProgress data={progress} />
        </div>
      </div>
    </div>
  );
}

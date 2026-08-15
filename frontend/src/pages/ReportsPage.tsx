import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Card } from '../ui/display';
import { Skeleton } from '../ui/Skeleton';
import StatusPie from '../components/charts/StatusPie';
import WorkloadBar from '../components/charts/WorkloadBar';
import ProjectProgress from '../components/charts/ProjectProgress';
import CompletionTrend from '../components/charts/CompletionTrend';
import { useCompletionTrend, useProjectProgress, useStatusBreakdown, useWorkloadChart } from '../lib/queries';
import styles from './pages.module.css';

export default function ReportsPage() {
  const { t } = useTranslation();

  const statusBreakdown = useStatusBreakdown();
  const workloadChart = useWorkloadChart();
  const projectProgress = useProjectProgress();
  const completionTrend = useCompletionTrend();

  return (
    <>
      <PageHeader title={t('reports.title')} />

      <div className={`${styles.grid} ${styles.gridTwo}`}>
        <Card>
          <h2 className={styles.chartTitle}>{t('dashboard.tasksByStatus')}</h2>
          {statusBreakdown.isLoading ? <Skeleton height={260} /> : <StatusPie data={statusBreakdown.data ?? []} />}
        </Card>

        <Card>
          <h2 className={styles.chartTitle}>{t('dashboard.workload')}</h2>
          {workloadChart.isLoading ? <Skeleton height={260} /> : <WorkloadBar data={workloadChart.data ?? []} />}
        </Card>

        <Card>
          <h2 className={styles.chartTitle}>{t('dashboard.projectProgress')}</h2>
          {projectProgress.isLoading ? <Skeleton height={260} /> : <ProjectProgress data={projectProgress.data ?? []} />}
        </Card>

        <Card>
          <h2 className={styles.chartTitle}>{t('dashboard.completions')}</h2>
          {completionTrend.isLoading ? <Skeleton height={260} /> : <CompletionTrend data={completionTrend.data ?? []} />}
        </Card>
      </div>
    </>
  );
}

import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Badge, Card, EmptyState, ErrorState } from '../ui/display';
import { SkeletonList } from '../ui/Skeleton';
import { CheckSquare } from '../ui/icons';
import { useMyTasks } from '../lib/queries';
import { formatDate, isOverdue, priorityTone } from '../lib/format';
import styles from './pages.module.css';

export default function MyTasksPage() {
  const { t } = useTranslation();
  const { data: tasks, isLoading, isError, refetch } = useMyTasks();

  return (
    <>
      <PageHeader title={t('myTasks.title')} />

      {isLoading && <SkeletonList rows={5} height={52} label={t('common.loading')} />}

      {isError && (
        <Card>
          <ErrorState title={t('errors.loadFailed')} onRetry={() => void refetch()} retryLabel={t('common.retry')} />
        </Card>
      )}

      {tasks?.length === 0 && (
        <Card>
          <EmptyState icon={<CheckSquare size={22} />} title={t('myTasks.empty')} body={t('myTasks.emptyBody')} />
        </Card>
      )}

      {tasks && tasks.length > 0 && (
        <Card padded={false}>
          <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
            {tasks.map((task) => {
              const project = typeof task.project === 'string' ? undefined : task.project;
              const overdue = isOverdue(task.dueDate, task.status);
              return (
                <li key={task.id} className={styles.taskRow}>
                  <div style={{ minWidth: 0 }}>
                    <div className={styles.taskTitle}>{task.title}</div>
                    {project && (
                      <Link to={`/projects/${project.id}`} className={styles.subtle}>
                        {project.name}
                      </Link>
                    )}
                  </div>
                  <div className={styles.taskMeta}>
                    <Badge tone={priorityTone(task.priority)}>{t(`task.priority${capitalize(task.priority)}`)}</Badge>
                    {task.dueDate && (
                      <Badge tone={overdue ? 'danger' : 'neutral'}>
                        {overdue ? `${t('task.overdue')} · ` : ''}
                        {formatDate(task.dueDate)}
                      </Badge>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        </Card>
      )}
    </>
  );
}

function capitalize(value: string) {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

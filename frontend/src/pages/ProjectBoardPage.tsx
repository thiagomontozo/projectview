import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Badge, Card, ErrorState } from '../ui/display';
import { Button } from '../ui/Button';
import { SkeletonList } from '../ui/Skeleton';
import { useToast } from '../ui/Toast';
import { Plus } from '../ui/icons';
import KanbanBoard from '../components/kanban/KanbanBoard';
import TaskModal from '../components/TaskModal';
import { useMoveTask, useProject, useProjectTasks, useUsers } from '../lib/queries';
import { statusOf } from '../lib/api';
import type { Task } from '../types';

interface ModalState {
  task?: Task;
  defaultStatus?: string;
}

export default function ProjectBoardPage() {
  const { id } = useParams<{ id: string }>();
  const { t } = useTranslation();
  const toast = useToast();

  const project = useProject(id);
  const tasks = useProjectTasks(id);
  const { data: users = [] } = useUsers();
  const moveTask = useMoveTask();

  const [modal, setModal] = useState<ModalState | null>(null);

  if (project.isError) {
    const notFound = statusOf(project.error) === 404;
    return (
      <Card>
        <ErrorState
          title={notFound ? t('errors.notFound') : t('errors.loadFailed')}
          body={notFound ? t('errors.notFoundBody') : t('errors.genericBody')}
          onRetry={notFound ? undefined : () => void project.refetch()}
          retryLabel={t('common.retry')}
        />
      </Card>
    );
  }

  if (project.isLoading || !project.data) {
    return <SkeletonList rows={3} height={120} label={t('common.loading')} />;
  }

  const columns = project.data.statuses.slice().sort((a, b) => a.order - b.order);
  // Sub-tasks live inside their parent's detail view, not as board cards.
  const boardTasks = (tasks.data ?? []).filter((task) => !task.parentTask);

  return (
    <>
      <PageHeader
        title={project.data.name}
        description={project.data.description}
        actions={
          <Button variant="primary" onClick={() => setModal({ defaultStatus: columns[0]?.key })}>
            <Plus size={16} />
            {t('board.newTask')}
          </Button>
        }
      />

      <div style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center', marginBottom: 'var(--space-4)' }}>
        <Link to="/projects">{t('board.backToProjects')}</Link>
        <Badge>{project.data.key}</Badge>
        <Badge tone="accent">{project.data.status}</Badge>
      </div>

      {tasks.isLoading ? (
        <SkeletonList rows={3} height={120} label={t('common.loading')} />
      ) : (
        <KanbanBoard
          columns={columns}
          tasks={boardTasks}
          onOpenTask={(task) => setModal({ task })}
          onAddTask={(statusKey) => setModal({ defaultStatus: statusKey })}
          onMove={(taskId, status, order) =>
            moveTask.mutate(
              { taskId, projectId: project.data.id, status, order },
              { onError: () => toast.error(t('board.moveFailed')) }
            )
          }
        />
      )}

      {modal && (
        <TaskModal
          project={project.data}
          task={modal.task}
          defaultStatus={modal.defaultStatus}
          users={users}
          onClose={() => setModal(null)}
        />
      )}
    </>
  );
}

import { useMemo, useState } from 'react';
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
import { BulkActionBar, ViewToolbar } from '../views/ViewToolbar';
import { ListView } from '../views/ListView';
import { TableView } from '../views/TableView';
import { CalendarView } from '../views/CalendarView';
import { TimelineView } from '../views/TimelineView';
import { WorkloadView } from '../views/WorkloadView';
import { applyView, useViewState } from '../views/useViewState';
import { useMoveTask, useProject, useProjectTasks, useSaveTask, useUsers } from '../lib/queries';
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
  const { data: allUsers = [] } = useUsers();
  const moveTask = useMoveTask();
  const saveTask = useSaveTask();

  const view = useViewState(id);
  const [modal, setModal] = useState<ModalState | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const columns = useMemo(
    () => (project.data?.statuses ?? []).slice().sort((a, b) => a.order - b.order),
    [project.data]
  );

  // Sub-tasks live inside their parent's detail view, not as top-level rows.
  const topLevel = useMemo(
    () => (tasks.data ?? []).filter((task) => !task.parentTask),
    [tasks.data]
  );

  const visible = useMemo(
    () => applyView(topLevel, view.state, columns.map((column) => column.key)),
    [topLevel, view.state, columns]
  );

  // Members of the project, falling back to everyone when the project keeps
  // no explicit membership list.
  const members = useMemo(() => {
    const projectMembers = project.data?.members ?? [];
    return projectMembers.length > 0 ? projectMembers : allUsers;
  }, [project.data, allUsers]);

  function toggleSelect(taskId: string) {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(taskId)) next.delete(taskId);
      else next.add(taskId);
      return next;
    });
  }

  function toggleSelectAll(taskIds: string[]) {
    setSelected((current) => (taskIds.every((id) => current.has(id)) ? new Set() : new Set(taskIds)));
  }

  function patchTask(taskId: string, patch: Record<string, unknown>) {
    saveTask.mutate(
      { id: taskId, body: patch },
      { onError: () => toast.error(t('errors.genericBody')) }
    );
  }

  function applyToSelection(patch: Record<string, unknown>) {
    const ids = [...selected];
    Promise.all(
      ids.map((taskId) => saveTask.mutateAsync({ id: taskId, body: patch }).catch(() => null))
    ).then((results) => {
      const failed = results.filter((result) => result === null).length;
      if (failed > 0) toast.error(t('views.bulkPartial', { failed, total: ids.length }));
      else toast.success(t('views.bulkApplied', { count: ids.length }));
      setSelected(new Set());
    });
  }

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

      <ViewToolbar
        state={view.state}
        statuses={columns}
        members={members}
        activeFilterCount={view.activeFilterCount}
        onKindChange={view.setKind}
        onGroupByChange={view.setGroupBy}
        onFiltersChange={view.setFilters}
        onClearFilters={view.clearFilters}
      />

      {tasks.isLoading ? (
        <SkeletonList rows={4} height={64} label={t('common.loading')} />
      ) : (
        <>
          {view.state.kind === 'board' && (
            <KanbanBoard
              columns={columns}
              tasks={visible}
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

          {view.state.kind === 'list' && (
            <ListView
              tasks={visible}
              state={view.state}
              statuses={columns}
              selected={selected}
              onToggleSelect={toggleSelect}
              onOpenTask={(task) => setModal({ task })}
            />
          )}

          {view.state.kind === 'table' && (
            <TableView
              tasks={visible}
              state={view.state}
              statuses={columns}
              selected={selected}
              onToggleSelect={toggleSelect}
              onToggleSelectAll={toggleSelectAll}
              onSort={view.setSort}
              onOpenTask={(task) => setModal({ task })}
              onPatchTask={patchTask}
            />
          )}

          {view.state.kind === 'calendar' && (
            <CalendarView tasks={visible} onOpenTask={(task) => setModal({ task })} />
          )}

          {view.state.kind === 'timeline' && (
            <TimelineView
              tasks={visible}
              onOpenTask={(task) => setModal({ task })}
              onReschedule={(taskId, startDate, dueDate) => patchTask(taskId, { startDate, dueDate })}
            />
          )}

          {view.state.kind === 'workload' && <WorkloadView tasks={visible} members={members} />}
        </>
      )}

      <BulkActionBar
        count={selected.size}
        statuses={columns}
        busy={saveTask.isPending}
        onStatusChange={(status) => applyToSelection({ status })}
        onPriorityChange={(priority) => applyToSelection({ priority })}
        onClear={() => setSelected(new Set())}
      />

      {modal && (
        <TaskModal
          project={project.data}
          task={modal.task}
          defaultStatus={modal.defaultStatus}
          users={members}
          onClose={() => setModal(null)}
        />
      )}
    </>
  );
}

import { Suspense, lazy, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../app/AppShell';
import { Badge, Card, ErrorState } from '../ui/display';
import { Button } from '../ui/Button';
import { SkeletonList } from '../ui/Skeleton';
import { useToast } from '../ui/Toast';
import { Inbox, Plus } from '../ui/icons';
import KanbanBoard from '../components/kanban/KanbanBoard';
import TaskModal from '../components/TaskModal';
import { BulkActionBar, ViewToolbar } from '../views/ViewToolbar';
import { ListView } from '../views/ListView';
import { TableView } from '../views/TableView';
import { CalendarView } from '../views/CalendarView';
import { TimelineView } from '../views/TimelineView';
import { WorkloadView } from '../views/WorkloadView';
import { useViewState } from '../views/useViewState';
import {
  pagedTasks,
  useMoveTask,
  useProject,
  useProjectTaskPage,
  useSaveTask,
  useSchedule,
  useCustomFields,
  useTaskCounts,
  useUsers
} from '../lib/queries';
import { errorMessage, statusOf } from '../lib/api';
import { useDeleteView, useSaveView, useSavedViews } from '../lib/canvas';
import type { Task } from '../types';

// Loaded on demand: the form builder is opened rarely and has no business
// sitting in the bundle every board render pays for.
const IntakeDialog = lazy(() => import('../components/IntakeDialog'));

interface ModalState {
  task?: Task;
  defaultStatus?: string;
  // Set when a task is added from inside a group that is not a status group,
  // so the value that was on screen is the value the task arrives with.
  defaultPriority?: string;
  defaultAssignee?: string;
}

export default function ProjectBoardPage() {
  const { id } = useParams<{ id: string }>();
  const { t } = useTranslation();
  const toast = useToast();

  const view = useViewState(id);
  const savedViews = useSavedViews(id);
  const saveView = useSaveView(id ?? '');
  const deleteView = useDeleteView(id ?? '');
  const customFields = useCustomFields(id);

  const project = useProject(id);
  const { data: allUsers = [] } = useUsers();
  const moveTask = useMoveTask();
  const saveTask = useSaveTask();

  // Totals per column, under whatever filters are applied. The board needs
  // them to say "100 of 3,412" rather than implying the hundred is all there
  // is; the other views use the sum for the same reason.
  // Keyed by the grouping in force, because the list's headers need a real
  // total per group just as the board's columns do.
  const counts = useTaskCounts(
    id,
    view.state.filters,
    view.state.kind === 'board' ? 'status' : (view.state.groupBy === 'none' ? undefined : view.state.groupBy)
  );

  // Every view except the board reads one paged stream rather than a page per
  // column. The board does not use this - its columns fetch their own - so it
  // is disabled there instead of being fetched and thrown away.
  // The calendar and the timeline are date-shaped, so they ask for the span
  // they draw rather than a page of whatever sorted first. A year around today
  // covers what either shows, and turns "100 of 3,412" into an answer that is
  // actually complete for the period on screen.
  const dateWindow = useMemo(() => {
    if (view.state.kind !== 'calendar' && view.state.kind !== 'timeline') return {};
    const from = new Date();
    from.setMonth(from.getMonth() - 6);
    const to = new Date();
    to.setMonth(to.getMonth() + 6);
    return { dueFrom: from.toISOString(), dueTo: to.toISOString() };
  }, [view.state.kind]);

  const flat = useProjectTaskPage(id, {
    filters: view.state.filters,
    sortBy: view.state.sortBy,
    sortDirection: view.state.sortDirection,
    enabled: view.state.kind !== 'board',
    ...dateWindow
  });
  const { tasks: visible, total, loaded } = pagedTasks(flat.data);
  // True when what is on screen is a subset of what matches. Every view that
  // aggregates - the calendar, the timeline, the workload grid - has to say so,
  // because a chart drawn from part of the data looks exactly like a chart
  // drawn from all of it.
  const partial = total > loaded;

  // Only the timeline draws arrows, so the schedule is not fetched elsewhere -
  // and it is scoped to the bars on screen, since an arrow needs two of them
  // and an edge with one end off-screen has nothing to connect. Declared after
  // `visible` because it depends on it.
  const schedule = useSchedule(
    view.state.kind === 'timeline' ? id : undefined,
    view.state.kind === 'timeline' ? visible.map((task) => task.id) : undefined
  );

  const [modal, setModal] = useState<ModalState | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [intakeOpen, setIntakeOpen] = useState(false);
  // Which custom fields are shown as columns. Per session rather than saved: a
  // column somebody added to answer one question should not become part of what
  // the project looks like for everybody.
  const [shownFields, setShownFields] = useState<string[]>([]);

  const columns = useMemo(
    () => (project.data?.statuses ?? []).slice().sort((a, b) => a.order - b.order),
    [project.data]
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
          <>
            <Button variant="ghost" onClick={() => setIntakeOpen(true)}>
              <Inbox size={16} />
              {t('intake.title')}
            </Button>
            <Button variant="primary" onClick={() => setModal({ defaultStatus: columns[0]?.key })}>
              <Plus size={16} />
              {t('board.newTask')}
            </Button>
          </>
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
        savedViews={savedViews.data ?? []}
        onApplyView={(saved) => view.apply({ ...saved, filters: saved.filters as Partial<typeof view.state.filters> })}
        onSaveView={(name) =>
          saveView.mutate(
            {
              name,
              kind: view.state.kind,
              groupBy: view.state.groupBy,
              // Widened once, here: the stored shape is deliberately open
              // (a view saved last year should still load after a filter is
              // added), while the interface's own filters are exact.
              filters: { ...view.state.filters },
              sortBy: view.state.sortBy,
              sortDirection: view.state.sortDirection
            },
            { onError: (error) => toast.error(errorMessage(error, t('errors.genericBody'))) }
          )
        }
        onDeleteView={(id) => deleteView.mutate(id)}
      />

      {/* Every view below the board draws from a page rather than the whole
          project, so it says what it is showing. The board says it per column
          instead, where the missing cards actually are. */}
      {view.state.kind !== 'board' && partial && (
        <p
          role="status"
          style={{
            fontSize: 'var(--text-sm)',
            color: 'var(--text-secondary)',
            marginBottom: 'var(--space-3)'
          }}
        >
          {t('views.showingOf', { loaded, total })}{' '}
          {flat.hasNextPage && (
            <Button variant="ghost" size="sm" loading={flat.isFetchingNextPage} onClick={() => void flat.fetchNextPage()}>
              {t('views.loadMore')}
            </Button>
          )}
        </p>
      )}

      {view.state.kind !== 'board' && flat.isLoading ? (
        <SkeletonList rows={4} height={64} label={t('common.loading')} />
      ) : (
        <>
          {view.state.kind === 'board' && (
            <KanbanBoard
              projectId={project.data.id}
              columns={columns}
              filters={view.state.filters}
              sortBy={view.state.sortBy}
              sortDirection={view.state.sortDirection}
              counts={counts.data?.byStatus}
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
              groupTotals={counts.data?.byGroup}
              members={members}
              selected={selected}
              onToggleSelect={toggleSelect}
              onOpenTask={(task) => setModal({ task })}
              // The group carries into the new task, because that is the
              // context somebody is already in: they are looking at "In
              // progress" and want one more thing in it.
              onAddInGroup={(group) =>
                setModal({
                  defaultStatus: group.by === 'status' ? group.key : columns[0]?.key,
                  defaultPriority: group.by === 'priority' ? group.key : undefined,
                  defaultAssignee: group.by === 'assignee' && group.key !== 'unassigned' ? group.key : undefined
                })
              }
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
              customFields={customFields.data ?? []}
              shownFields={shownFields}
              onShownFieldsChange={setShownFields}
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
              dependencies={schedule.data?.dependencies}
              criticalPath={schedule.data?.criticalPath}
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

      {intakeOpen && (
        <Suspense fallback={null}>
          <IntakeDialog projectId={project.data.id} onClose={() => setIntakeOpen(false)} />
        </Suspense>
      )}

      {modal && (
        <TaskModal
          project={project.data}
          task={modal.task}
          defaultStatus={modal.defaultStatus}
          defaultPriority={modal.defaultPriority}
          defaultAssignee={modal.defaultAssignee}
          users={members}
          onClose={() => setModal(null)}
        />
      )}
    </>
  );
}

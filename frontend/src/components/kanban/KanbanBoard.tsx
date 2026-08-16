import { useState } from 'react';
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  closestCorners,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent
} from '@dnd-kit/core';
import { SortableContext, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import styles from './kanban.module.css';
import { Avatar, AvatarGroup, Badge } from '../../ui/display';
import { Button } from '../../ui/Button';
import { Plus } from '../../ui/icons';
import { formatDate, isOverdue, priorityTone } from '../../lib/format';
import { pagedTasks, useProjectTaskPage, type TaskFilterInput } from '../../lib/queries';
import type { ProjectStatusColumn, Task } from '../../types';

/**
 * The kanban board.
 *
 * Each column fetches its own page rather than the board fetching every task in
 * the project and dealing them out. The old shape was measured at 10.1 MB and
 * nine seconds for a 10,000-task project, and it slowed every other screen down
 * beside it; a column now asks for a hundred cards and says how many there are.
 *
 * Because the columns own their data, nothing here holds a list of every task -
 * so the drag handlers cannot look one up by id. Instead each card carries its
 * own task on the drag event and each column carries the order to append at,
 * which is both simpler and correct: a drop only ever needs the thing dragged
 * and the place it landed.
 */

interface Props {
  projectId: string;
  columns: ProjectStatusColumn[];
  filters: TaskFilterInput;
  sortBy: string;
  sortDirection: 'asc' | 'desc';
  counts: Record<string, number> | undefined;
  onOpenTask: (task: Task) => void;
  onAddTask: (statusKey: string) => void;
  onMove: (taskId: string, newStatus: string, newOrder: number) => void;
}

interface DragData {
  task?: Task;
  status?: string;
  appendOrder?: number;
}

export default function KanbanBoard({
  projectId,
  columns,
  filters,
  sortBy,
  sortDirection,
  counts,
  onOpenTask,
  onAddTask,
  onMove
}: Props) {
  const [activeTask, setActiveTask] = useState<Task | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));

  function handleDragStart(event: DragStartEvent) {
    setActiveTask((event.active.data.current as DragData | undefined)?.task ?? null);
  }

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    setActiveTask(null);
    if (!over) return;

    const dragged = (active.data.current as DragData | undefined)?.task;
    if (!dragged) return;

    const target = over.data.current as DragData | undefined;
    // Dropped on a column: append. Dropped on a card: take that card's place.
    const newStatus = target?.status ?? target?.task?.status;
    if (!newStatus) return;

    const newOrder = target?.task ? target.task.order : (target?.appendOrder ?? 0);
    if (newStatus === dragged.status && newOrder === dragged.order) return;

    onMove(dragged.id, newStatus, newOrder);
  }

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCorners}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
    >
      <div className={styles.board}>
        {columns.map((column) => (
          <KanbanColumn
            key={column.key}
            projectId={projectId}
            column={column}
            filters={filters}
            sortBy={sortBy}
            sortDirection={sortDirection}
            total={counts?.[column.key]}
            onOpenTask={onOpenTask}
            onAddTask={onAddTask}
          />
        ))}
      </div>

      {/* Rendered outside the columns so the dragged card is not clipped by
          the scrolling drop zone. */}
      <DragOverlay>{activeTask && <KanbanCard task={activeTask} onOpen={() => {}} overlay />}</DragOverlay>
    </DndContext>
  );
}

function KanbanColumn({
  projectId,
  column,
  filters,
  sortBy,
  sortDirection,
  total,
  onOpenTask,
  onAddTask
}: {
  projectId: string;
  column: ProjectStatusColumn;
  filters: TaskFilterInput;
  sortBy: string;
  sortDirection: 'asc' | 'desc';
  total: number | undefined;
  onOpenTask: (task: Task) => void;
  onAddTask: (statusKey: string) => void;
}) {
  const { t } = useTranslation();

  const page = useProjectTaskPage(projectId, {
    filters,
    sortBy,
    sortDirection,
    status: column.key
  });
  const { tasks, loaded } = pagedTasks(page.data);

  // The count comes from the counts endpoint, which counts the whole column
  // under the same filters. Falling back to what is loaded is only for the
  // moment before it arrives - never a substitute for it, or a column would
  // claim to hold exactly what it happens to be showing.
  const columnTotal = total ?? loaded;
  const appendOrder = (tasks.at(-1)?.order ?? -1) + 1;

  const { setNodeRef, isOver } = useDroppable({
    id: column.key,
    data: { status: column.key, appendOrder } satisfies DragData
  });

  return (
    <section className={styles.column} aria-label={column.label}>
      <header className={styles.columnHeader}>
        <span className={styles.columnDot} style={{ background: column.color }} aria-hidden="true" />
        <h3 className={styles.columnLabel}>{column.label}</h3>
        <span className={styles.columnCount}>{columnTotal}</span>
        <Button
          variant="ghost"
          size="sm"
          iconOnly
          className={styles.columnAdd}
          aria-label={t('board.addToColumn', { column: column.label })}
          onClick={() => onAddTask(column.key)}
        >
          <Plus size={15} />
        </Button>
      </header>

      <div ref={setNodeRef} className={clsx(styles.dropZone, isOver && styles.dropZoneOver)}>
        <SortableContext items={tasks.map((task) => task.id)} strategy={verticalListSortingStrategy}>
          {tasks.map((task) => (
            <KanbanCard key={task.id} task={task} onOpen={onOpenTask} />
          ))}
        </SortableContext>

        {page.isLoading && <p className={styles.emptyColumn}>{t('common.loading')}</p>}
        {!page.isLoading && tasks.length === 0 && <p className={styles.emptyColumn}>{t('board.noTasks')}</p>}

        {/* Says what is missing rather than implying nothing is. A column
            showing its first hundred cards with no hint that three thousand
            exist is the failure this whole change is about. */}
        {page.hasNextPage && (
          <Button
            variant="ghost"
            size="sm"
            block
            loading={page.isFetchingNextPage}
            onClick={() => void page.fetchNextPage()}
          >
            {t('board.loadMore', { loaded, total: columnTotal })}
          </Button>
        )}
      </div>
    </section>
  );
}

function KanbanCard({
  task,
  onOpen,
  overlay
}: {
  task: Task;
  onOpen: (task: Task) => void;
  overlay?: boolean;
}) {
  const { t } = useTranslation();
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: task.id,
    disabled: overlay,
    // Carried on the event so a drop needs no lookup table. With the columns
    // paging independently there is no longer one list holding every task.
    data: { task } satisfies DragData
  });

  const overdue = isOverdue(task.dueDate, task.status);

  return (
    <button
      ref={overlay ? undefined : setNodeRef}
      type="button"
      className={clsx(styles.card, isDragging && styles.cardDragging)}
      style={overlay ? undefined : { transform: CSS.Transform.toString(transform), transition }}
      onClick={() => onOpen(task)}
      {...(overlay ? {} : attributes)}
      {...(overlay ? {} : listeners)}
    >
      <span className={styles.cardTitle}>{task.title}</span>

      <span className={styles.cardMeta}>
        <Badge tone={priorityTone(task.priority)}>
          {t(`task.priority${task.priority.charAt(0).toUpperCase()}${task.priority.slice(1)}`)}
        </Badge>
        {task.dueDate && (
          <Badge tone={overdue ? 'danger' : 'neutral'}>
            {overdue ? `${t('task.overdue')} · ` : ''}
            {formatDate(task.dueDate)}
          </Badge>
        )}
        {task.subtaskCount > 0 && <Badge>{task.subtaskCount}</Badge>}
      </span>

      {task.assignees.length > 0 && (
        <span className={styles.cardFooter}>
          <AvatarGroup>
            {task.assignees.slice(0, 3).map((assignee) => (
              <Avatar key={assignee.id} name={assignee.name} color={assignee.avatarColor} size={22} />
            ))}
          </AvatarGroup>
        </span>
      )}
    </button>
  );
}

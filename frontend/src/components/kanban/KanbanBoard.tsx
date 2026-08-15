import { useMemo, useState } from 'react';
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
import type { ProjectStatusColumn, Task } from '../../types';

interface Props {
  columns: ProjectStatusColumn[];
  tasks: Task[];
  onOpenTask: (task: Task) => void;
  onAddTask: (statusKey: string) => void;
  onMove: (taskId: string, newStatus: string, newOrder: number) => void;
}

export default function KanbanBoard({ columns, tasks, onOpenTask, onAddTask, onMove }: Props) {
  const { t } = useTranslation();
  const [activeTask, setActiveTask] = useState<Task | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));

  const byColumn = useMemo(() => {
    const map: Record<string, Task[]> = {};
    columns.forEach((column) => (map[column.key] = []));
    tasks
      .slice()
      .sort((a, b) => a.order - b.order)
      .forEach((task) => {
        if (!map[task.status]) map[task.status] = [];
        map[task.status].push(task);
      });
    return map;
  }, [columns, tasks]);

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    setActiveTask(null);
    if (!over) return;

    const dragged = tasks.find((task) => task.id === active.id);
    if (!dragged) return;

    const overIsColumn = columns.some((column) => column.key === over.id);
    const newStatus = overIsColumn ? String(over.id) : tasks.find((task) => task.id === over.id)?.status;
    if (!newStatus) return;

    const columnTasks = byColumn[newStatus] ?? [];
    const newOrder = overIsColumn
      ? (columnTasks.at(-1)?.order ?? -1) + 1
      : (tasks.find((task) => task.id === over.id)?.order ?? 0);

    if (newStatus === dragged.status && newOrder === dragged.order) return;
    onMove(dragged.id, newStatus, newOrder);
  }

  function handleDragStart(event: DragStartEvent) {
    setActiveTask(tasks.find((task) => task.id === event.active.id) ?? null);
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
            column={column}
            tasks={byColumn[column.key] ?? []}
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
  column,
  tasks,
  onOpenTask,
  onAddTask
}: {
  column: ProjectStatusColumn;
  tasks: Task[];
  onOpenTask: (task: Task) => void;
  onAddTask: (statusKey: string) => void;
}) {
  const { t } = useTranslation();
  const { setNodeRef, isOver } = useDroppable({ id: column.key, data: { status: column.key } });

  return (
    <section className={styles.column} aria-label={column.label}>
      <header className={styles.columnHeader}>
        <span className={styles.columnDot} style={{ background: column.color }} aria-hidden="true" />
        <h3 className={styles.columnLabel}>{column.label}</h3>
        <span className={styles.columnCount}>{tasks.length}</span>
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
        {tasks.length === 0 && <p className={styles.emptyColumn}>{t('board.noTasks')}</p>}
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
    disabled: overlay
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

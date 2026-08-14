import { useMemo, useState } from 'react';
import { DndContext, DragOverlay, PointerSensor, useSensor, useSensors, closestCorners, type DragEndEvent, type DragStartEvent } from '@dnd-kit/core';
import KanbanColumn from './KanbanColumn.tsx';
import KanbanCard from './KanbanCard.tsx';
import type { ProjectStatusColumn, Task } from '../../types';

interface Props {
  columns: ProjectStatusColumn[];
  tasks: Task[];
  onOpenTask: (task: Task) => void;
  onAddTask: (statusKey: string) => void;
  onMove: (taskId: string, newStatus: string, newOrder: number) => void;
}

/**
 * Renders the draggable card board and persists status/order changes via
 * onMove(taskId, newStatus, newOrder). Optimistic local reordering keeps the
 * drag feeling instant while the PATCH request happens in the background.
 */
export default function KanbanBoard({ columns, tasks, onOpenTask, onAddTask, onMove }: Props) {
  const [activeTask, setActiveTask] = useState<Task | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));

  const byColumn = useMemo(() => {
    const map: Record<string, Task[]> = {};
    columns.forEach((c) => (map[c.key] = []));
    tasks
      .slice()
      .sort((a, b) => a.order - b.order)
      .forEach((t) => {
        if (!map[t.status]) map[t.status] = [];
        map[t.status].push(t);
      });
    return map;
  }, [columns, tasks]);

  function handleDragStart(event: DragStartEvent) {
    const task = tasks.find((t) => t.id === event.active.id);
    setActiveTask(task || null);
  }

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    setActiveTask(null);
    if (!over) return;

    const activeTaskObj = tasks.find((t) => t.id === active.id);
    if (!activeTaskObj) return;

    const overIsColumn = columns.some((c) => c.key === over.id);
    const newStatus = overIsColumn ? String(over.id) : tasks.find((t) => t.id === over.id)?.status;
    if (!newStatus) return;

    const columnTasks = byColumn[newStatus] || [];
    let newOrder: number;
    if (overIsColumn) {
      newOrder = columnTasks.length ? columnTasks[columnTasks.length - 1].order + 1 : 0;
    } else {
      const overTask = tasks.find((t) => t.id === over.id);
      newOrder = overTask ? overTask.order : 0;
    }

    if (newStatus === activeTaskObj.status && newOrder === activeTaskObj.order) return;
    onMove(activeTaskObj.id, newStatus, newOrder);
  }

  return (
    <DndContext sensors={sensors} collisionDetection={closestCorners} onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
      <div style={{ display: 'flex', gap: 16, overflowX: 'auto', paddingBottom: 12, height: 'calc(100vh - 190px)' }}>
        {columns.map((col) => (
          <KanbanColumn key={col.key} column={col} tasks={byColumn[col.key] || []} onOpenTask={onOpenTask} onAddTask={onAddTask} />
        ))}
      </div>
      <DragOverlay>{activeTask ? <KanbanCard task={activeTask} onOpen={() => {}} /> : null}</DragOverlay>
    </DndContext>
  );
}

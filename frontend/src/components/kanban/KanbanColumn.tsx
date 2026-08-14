import { useDroppable } from '@dnd-kit/core';
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable';
import KanbanCard from './KanbanCard.tsx';
import type { ProjectStatusColumn, Task } from '../../types';

interface Props {
  column: ProjectStatusColumn;
  tasks: Task[];
  onOpenTask: (task: Task) => void;
  onAddTask: (statusKey: string) => void;
}

export default function KanbanColumn({ column, tasks, onOpenTask, onAddTask }: Props) {
  const { setNodeRef, isOver } = useDroppable({ id: column.key, data: { status: column.key } });

  return (
    <div style={{ width: 288, flexShrink: 0, display: 'flex', flexDirection: 'column', maxHeight: '100%' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 6px 10px' }}>
        <span style={{ width: 9, height: 9, borderRadius: '50%', background: column.color }} />
        <strong style={{ fontSize: 13 }}>{column.label}</strong>
        <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{tasks.length}</span>
        <button className="btn btn-sm" style={{ marginLeft: 'auto', padding: '2px 8px' }} onClick={() => onAddTask(column.key)}>
          +
        </button>
      </div>
      <div
        ref={setNodeRef}
        style={{
          background: isOver ? '#eef4fc' : 'transparent',
          borderRadius: 10,
          padding: 6,
          flex: 1,
          overflowY: 'auto',
          minHeight: 60,
          transition: 'background 0.15s'
        }}
      >
        <SortableContext items={tasks.map((t) => t.id)} strategy={verticalListSortingStrategy}>
          {tasks.map((task) => (
            <KanbanCard key={task.id} task={task} onOpen={onOpenTask} />
          ))}
        </SortableContext>
        {tasks.length === 0 && <div style={{ fontSize: 12, color: 'var(--muted)', padding: '10px 6px' }}>Nenhuma tarefa</div>}
      </div>
    </div>
  );
}

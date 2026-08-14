import type { CSSProperties } from 'react';
import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { priorityColor } from '../../styles/theme.ts';
import type { Task } from '../../types';

function initials(name = ''): string {
  return name
    .split(' ')
    .map((p) => p[0])
    .slice(0, 2)
    .join('')
    .toUpperCase();
}

function isOverdue(task: Task): boolean {
  return Boolean(task.dueDate) && task.status !== 'done' && new Date(task.dueDate as string) < new Date();
}

export default function KanbanCard({ task, onOpen }: { task: Task; onOpen: (task: Task) => void }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: task.id,
    data: { status: task.status }
  });

  const style: CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition: transition ?? undefined,
    opacity: isDragging ? 0.5 : 1
  };

  return (
    <div
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      onClick={() => onOpen(task)}
      className="card"
      style={{ ...style, padding: '12px 12px 10px', marginBottom: 10, cursor: 'grab' }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 6, marginBottom: 8 }}>
        <span
          className="badge"
          style={{ background: `${priorityColor[task.priority] || '#898781'}22`, color: priorityColor[task.priority] || '#898781' }}
        >
          {task.priority}
        </span>
        {isOverdue(task) && (
          <span className="badge" style={{ background: '#fdeceb', color: '#d03b3b' }}>
            atrasada
          </span>
        )}
      </div>
      <div style={{ fontSize: 14, fontWeight: 600, marginBottom: 8, lineHeight: 1.3 }}>{task.title}</div>

      {task.dueDate && (
        <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 8 }}>
          Prazo: {new Date(task.dueDate).toLocaleDateString('pt-BR')}
        </div>
      )}

      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div style={{ display: 'flex' }}>
          {(task.assignees || []).slice(0, 4).map((a, i) => (
            <div
              key={a.id}
              className="avatar"
              title={a.name}
              style={{
                background: a.avatarColor || '#2a78d6',
                width: 22,
                height: 22,
                fontSize: 10,
                marginLeft: i === 0 ? 0 : -8,
                border: '2px solid var(--surface-1)'
              }}
            >
              {initials(a.name)}
            </div>
          ))}
        </div>
        {task.subtaskCount > 0 && <span style={{ fontSize: 11, color: 'var(--text-secondary)' }}>{task.subtaskCount} sub-tarefas</span>}
      </div>
    </div>
  );
}

import { useEffect, useState, useCallback } from 'react';
import { useParams, Link } from 'react-router-dom';
import api from '../api/client';
import KanbanBoard from '../components/kanban/KanbanBoard.tsx';
import TaskModal from '../components/TaskModal.tsx';
import type { Project, PublicUser, Task } from '../types';

interface ModalState {
  task?: Task;
  defaultStatus?: string;
}

export default function ProjectBoardPage() {
  const { id } = useParams<{ id: string }>();
  const [project, setProject] = useState<Project | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [users, setUsers] = useState<PublicUser[]>([]);
  const [modalState, setModalState] = useState<ModalState | null>(null);

  const load = useCallback(async () => {
    if (!id) return;
    const [p, t] = await Promise.all([api.get<Project>(`/projects/${id}`), api.get<Task[]>(`/projects/${id}/tasks`)]);
    setProject(p.data);
    setTasks(t.data.filter((task) => !task.parentTask));
  }, [id]);

  useEffect(() => {
    load();
    api.get<PublicUser[]>('/users').then((res) => setUsers(res.data));
  }, [load]);

  async function handleMove(taskId: string, status: string, order: number) {
    setTasks((prev) => prev.map((t) => (t.id === taskId ? { ...t, status, order } : t)));
    await api.patch(`/tasks/${taskId}/move`, { status, order });
  }

  function openTask(task: Task) {
    setModalState({ task });
  }

  function addTask(statusKey: string) {
    setModalState({ defaultStatus: statusKey });
  }

  async function handleSaved() {
    setModalState(null);
    await load();
  }

  if (!project) return <div>Carregando...</div>;

  return (
    <div>
      <div className="page-header">
        <div>
          <Link to="/projects" style={{ fontSize: 13, color: 'var(--text-secondary)' }}>
            ← Projetos
          </Link>
          <h1 style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ width: 12, height: 12, borderRadius: '50%', background: project.color }} />
            {project.name}
            <span className="badge">{project.key}</span>
          </h1>
        </div>
        <button className="btn btn-primary" onClick={() => addTask(project.statuses[0].key)}>
          + Nova tarefa
        </button>
      </div>

      <KanbanBoard
        columns={project.statuses.slice().sort((a, b) => a.order - b.order)}
        tasks={tasks}
        onOpenTask={openTask}
        onAddTask={addTask}
        onMove={handleMove}
      />

      {modalState && (
        <TaskModal
          project={project}
          task={modalState.task}
          defaultStatus={modalState.defaultStatus}
          users={users}
          onClose={() => setModalState(null)}
          onSaved={handleSaved}
          onDeleted={handleSaved}
        />
      )}
    </div>
  );
}

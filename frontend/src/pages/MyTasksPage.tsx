import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import api from '../api/client';
import { priorityColor } from '../styles/theme.ts';
import type { ProjectRefLite, Task } from '../types';

export default function MyTasksPage() {
  const [tasks, setTasks] = useState<Task[]>([]);

  useEffect(() => {
    api.get<Task[]>('/tasks/mine').then((res) => setTasks(res.data));
  }, []);

  return (
    <div>
      <div className="page-header">
        <h1>Minhas Tarefas</h1>
      </div>
      <div className="card" style={{ padding: 0 }}>
        <table className="table">
          <thead>
            <tr>
              <th>Tarefa</th>
              <th>Projeto</th>
              <th>Status</th>
              <th>Prioridade</th>
              <th>Prazo</th>
            </tr>
          </thead>
          <tbody>
            {tasks.map((t) => {
              const project = typeof t.project === 'object' ? (t.project as ProjectRefLite) : null;
              return (
                <tr key={t.id}>
                  <td>{t.title}</td>
                  <td>
                    {project && (
                      <Link to={`/projects/${project.id}`} className="badge">
                        {project.key}
                      </Link>
                    )}
                  </td>
                  <td>
                    <span className="badge">{t.status}</span>
                  </td>
                  <td>
                    <span className="badge" style={{ color: priorityColor[t.priority], background: `${priorityColor[t.priority]}22` }}>
                      {t.priority}
                    </span>
                  </td>
                  <td>
                    {t.dueDate ? (
                      <span style={{ color: new Date(t.dueDate) < new Date() && t.status !== 'done' ? '#d03b3b' : undefined }}>
                        {new Date(t.dueDate).toLocaleDateString('pt-BR')}
                      </span>
                    ) : (
                      '—'
                    )}
                  </td>
                </tr>
              );
            })}
            {tasks.length === 0 && (
              <tr>
                <td colSpan={5} style={{ color: 'var(--text-secondary)' }}>
                  Nenhuma tarefa atribuída a você ainda.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

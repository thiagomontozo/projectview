import { useEffect, useState } from 'react';
import api from '../api/client';
import type { WorkloadRow } from '../types';

function initials(name = ''): string {
  return name
    .split(' ')
    .map((p) => p[0])
    .slice(0, 2)
    .join('')
    .toUpperCase();
}

export default function ResourceAllocationPage() {
  const [rows, setRows] = useState<WorkloadRow[]>([]);

  useEffect(() => {
    api.get<WorkloadRow[]>('/users/workload').then((res) => setRows(res.data));
  }, []);

  return (
    <div>
      <div className="page-header">
        <h1>Alocação de Recursos</h1>
      </div>
      <p style={{ color: 'var(--text-secondary)', maxWidth: 640, marginTop: -8 }}>
        Cada recurso pode estar alocado em várias tarefas e projetos simultaneamente. A tabela abaixo mostra a carga de
        trabalho atual (tarefas abertas, horas estimadas e projetos) de cada pessoa.
      </p>

      <div className="card" style={{ padding: 0, marginTop: 16 }}>
        <table className="table">
          <thead>
            <tr>
              <th>Recurso</th>
              <th>Tarefas abertas</th>
              <th>Horas estimadas</th>
              <th>Projetos</th>
              <th>Atrasadas</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.user.id}>
                <td>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <div className="avatar" style={{ background: r.user.avatarColor || '#2a78d6' }}>
                      {initials(r.user.name)}
                    </div>
                    <div>
                      <div style={{ fontWeight: 600 }}>{r.user.name}</div>
                      <div style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{r.user.title || r.user.role}</div>
                    </div>
                  </div>
                </td>
                <td>{r.openTasks}</td>
                <td>{r.estimateHours}h</td>
                <td>{r.projectCount}</td>
                <td>
                  {r.overdue > 0 ? (
                    <span className="badge" style={{ background: '#fdeceb', color: '#d03b3b' }}>
                      {r.overdue}
                    </span>
                  ) : (
                    0
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

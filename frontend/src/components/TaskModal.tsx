import { useEffect, useState } from 'react';
import api from '../api/client';
import type { Project, PublicUser, Task } from '../types';

function toDateInput(value?: string): string {
  if (!value) return '';
  return new Date(value).toISOString().slice(0, 10);
}

interface Props {
  project: Project;
  task?: Task;
  parentTask?: string;
  defaultStatus?: string;
  users: PublicUser[];
  onClose: () => void;
  onSaved: () => void;
  onDeleted: () => void;
}

/**
 * Handles both creating a new task/subtask and editing an existing one.
 * `parentTask` (optional) marks this as a sub-task of another task.
 */
export default function TaskModal({ project, task, parentTask, defaultStatus, users, onClose, onSaved, onDeleted }: Props) {
  const isEdit = Boolean(task && task.id);
  const [form, setForm] = useState({
    title: task?.title || '',
    description: task?.description || '',
    status: task?.status || defaultStatus || project.statuses[0].key,
    priority: task?.priority || 'medium',
    assignees: task?.assignees?.map((a) => a.id) || ([] as string[]),
    startDate: toDateInput(task?.startDate),
    dueDate: toDateInput(task?.dueDate),
    estimateHours: task?.estimateHours || 0
  });
  const [subtasks, setSubtasks] = useState<Task[]>(task?.subtasks || []);
  const [comments, setComments] = useState(task?.comments || []);
  const [newSubtask, setNewSubtask] = useState('');
  const [comment, setComment] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (isEdit && task) {
      api.get(`/tasks/${task.id}`).then((res) => setSubtasks(res.data.subtasks || []));
    }
  }, [isEdit, task?.id]);

  function update<K extends keyof typeof form>(key: K, value: (typeof form)[K]) {
    setForm((f) => ({ ...f, [key]: value }));
  }

  function toggleAssignee(id: string) {
    setForm((f) => ({
      ...f,
      assignees: f.assignees.includes(id) ? f.assignees.filter((a) => a !== id) : [...f.assignees, id]
    }));
  }

  async function handleSave() {
    if (!form.title.trim()) return;
    setSaving(true);
    try {
      const payload = { ...form, project: project.id, parentTask: parentTask || task?.parentTask || null };
      if (isEdit && task) {
        await api.put(`/tasks/${task.id}`, payload);
      } else {
        await api.post('/tasks', payload);
      }
      onSaved();
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete() {
    if (!isEdit || !task) return;
    if (!confirm('Excluir esta tarefa e suas sub-tarefas?')) return;
    await api.delete(`/tasks/${task.id}`);
    onDeleted();
  }

  async function addSubtask() {
    if (!newSubtask.trim() || !task) return;
    const res = await api.post('/tasks', {
      title: newSubtask,
      project: project.id,
      parentTask: task.id,
      status: project.statuses[0].key
    });
    setSubtasks((s) => [...s, res.data]);
    setNewSubtask('');
  }

  async function addComment() {
    if (!comment.trim() || !task) return;
    const res = await api.post(`/tasks/${task.id}/comments`, { body: comment });
    setComment('');
    setComments(res.data.comments || []);
  }

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal">
        <div className="modal-header">
          <h2 style={{ margin: 0, fontSize: 18 }}>{isEdit ? 'Editar Tarefa' : parentTask ? 'Nova Sub-tarefa' : 'Nova Tarefa'}</h2>
          <button className="btn btn-sm" onClick={onClose}>
            Fechar
          </button>
        </div>

        <div className="form-row">
          <label className="label">Título</label>
          <input className="input" value={form.title} onChange={(e) => update('title', e.target.value)} autoFocus />
        </div>

        <div className="form-row">
          <label className="label">Descrição</label>
          <textarea className="textarea" value={form.description} onChange={(e) => update('description', e.target.value)} />
        </div>

        <div className="form-grid-2">
          <div className="form-row">
            <label className="label">Status</label>
            <select className="select" value={form.status} onChange={(e) => update('status', e.target.value)}>
              {project.statuses.map((s) => (
                <option key={s.key} value={s.key}>
                  {s.label}
                </option>
              ))}
            </select>
          </div>
          <div className="form-row">
            <label className="label">Prioridade</label>
            <select className="select" value={form.priority} onChange={(e) => update('priority', e.target.value as typeof form.priority)}>
              <option value="low">Baixa</option>
              <option value="medium">Média</option>
              <option value="high">Alta</option>
              <option value="urgent">Urgente</option>
            </select>
          </div>
        </div>

        <div className="form-grid-2">
          <div className="form-row">
            <label className="label">Início</label>
            <input className="input" type="date" value={form.startDate} onChange={(e) => update('startDate', e.target.value)} />
          </div>
          <div className="form-row">
            <label className="label">Prazo (fim)</label>
            <input className="input" type="date" value={form.dueDate} onChange={(e) => update('dueDate', e.target.value)} />
          </div>
        </div>

        <div className="form-row">
          <label className="label">Estimativa (horas)</label>
          <input
            className="input"
            type="number"
            min={0}
            value={form.estimateHours}
            onChange={(e) => update('estimateHours', Number(e.target.value))}
          />
        </div>

        <div className="form-row">
          <label className="label">Recursos alocados (podem estar em vários projetos/tarefas)</label>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
            {users.map((u) => (
              <button
                type="button"
                key={u.id}
                className="btn btn-sm"
                onClick={() => toggleAssignee(u.id)}
                style={{
                  background: form.assignees.includes(u.id) ? '#eef4fc' : undefined,
                  borderColor: form.assignees.includes(u.id) ? '#2a78d6' : undefined
                }}
              >
                {u.name}
              </button>
            ))}
          </div>
        </div>

        {isEdit && (
          <div className="form-row">
            <label className="label">Sub-tarefas</label>
            {subtasks.map((s) => (
              <div key={s.id} style={{ fontSize: 13, padding: '4px 0', borderBottom: '1px solid var(--grid)' }}>
                {s.title} <span className="badge">{s.status}</span>
              </div>
            ))}
            <div style={{ display: 'flex', gap: 6, marginTop: 8 }}>
              <input
                className="input"
                placeholder="Adicionar sub-tarefa..."
                value={newSubtask}
                onChange={(e) => setNewSubtask(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && addSubtask()}
              />
              <button className="btn btn-sm" onClick={addSubtask}>
                Adicionar
              </button>
            </div>
          </div>
        )}

        {isEdit && (
          <div className="form-row">
            <label className="label">Comentários</label>
            {comments.map((c) => (
              <div key={c.id} style={{ fontSize: 13, padding: '6px 0', borderBottom: '1px solid var(--grid)' }}>
                <strong>{c.author?.name || 'Usuário'}:</strong> {c.body}
              </div>
            ))}
            <div style={{ display: 'flex', gap: 6, marginTop: 8 }}>
              <input
                className="input"
                placeholder="Escrever um comentário..."
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && addComment()}
              />
              <button className="btn btn-sm" onClick={addComment}>
                Enviar
              </button>
            </div>
          </div>
        )}

        <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 20 }}>
          {isEdit ? (
            <button className="btn btn-sm" style={{ color: '#d03b3b' }} onClick={handleDelete}>
              Excluir
            </button>
          ) : (
            <span />
          )}
          <button className="btn btn-primary" onClick={handleSave} disabled={saving}>
            {saving ? 'Salvando...' : 'Salvar'}
          </button>
        </div>
      </div>
    </div>
  );
}

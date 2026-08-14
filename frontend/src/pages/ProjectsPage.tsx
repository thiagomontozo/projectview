import { useEffect, useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import api from '../api/client';
import { categorical } from '../styles/theme.ts';
import type { Project, PublicUser, Team } from '../types';

export default function ProjectsPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [teams, setTeams] = useState<Team[]>([]);
  const [users, setUsers] = useState<PublicUser[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ name: '', key: '', description: '', team: '', memberIds: [] as string[] });

  function load() {
    api.get<Project[]>('/projects').then((res) => setProjects(res.data));
  }

  useEffect(() => {
    load();
    api.get<Team[]>('/teams').then((res) => setTeams(res.data));
    api.get<PublicUser[]>('/users').then((res) => setUsers(res.data));
  }, []);

  async function createProject(e: FormEvent) {
    e.preventDefault();
    await api.post('/projects', form);
    setForm({ name: '', key: '', description: '', team: '', memberIds: [] });
    setShowForm(false);
    load();
  }

  return (
    <div>
      <div className="page-header">
        <h1>Projetos</h1>
        <button className="btn btn-primary" onClick={() => setShowForm((s) => !s)}>
          + Novo projeto
        </button>
      </div>

      {showForm && (
        <form className="card" style={{ padding: 18, marginBottom: 20 }} onSubmit={createProject}>
          <div className="form-grid-2">
            <div className="form-row">
              <label className="label">Nome</label>
              <input className="input" required value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />
            </div>
            <div className="form-row">
              <label className="label">Chave (ex: PMD)</label>
              <input
                className="input"
                required
                value={form.key}
                onChange={(e) => setForm((f) => ({ ...f, key: e.target.value.toUpperCase() }))}
              />
            </div>
          </div>
          <div className="form-row">
            <label className="label">Descrição</label>
            <textarea className="textarea" value={form.description} onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))} />
          </div>
          <div className="form-row">
            <label className="label">Equipe</label>
            <select className="select" value={form.team} onChange={(e) => setForm((f) => ({ ...f, team: e.target.value }))}>
              <option value="">Nenhuma</option>
              {teams.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </div>
          <div className="form-row">
            <label className="label">Membros</label>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {users.map((u) => (
                <button
                  type="button"
                  key={u.id}
                  className="btn btn-sm"
                  onClick={() =>
                    setForm((f) => ({
                      ...f,
                      memberIds: f.memberIds.includes(u.id) ? f.memberIds.filter((id) => id !== u.id) : [...f.memberIds, u.id]
                    }))
                  }
                  style={{
                    background: form.memberIds.includes(u.id) ? '#eef4fc' : undefined,
                    borderColor: form.memberIds.includes(u.id) ? '#2a78d6' : undefined
                  }}
                >
                  {u.name}
                </button>
              ))}
            </div>
          </div>
          <button className="btn btn-primary" type="submit">
            Criar
          </button>
        </form>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))', gap: 16 }}>
        {projects.map((p, i) => (
          <Link key={p.id} to={`/projects/${p.id}`} className="card" style={{ padding: 18, display: 'block' }}>
            <div
              style={{ width: 36, height: 36, borderRadius: 9, background: p.color || categorical[i % categorical.length], marginBottom: 12 }}
            />
            <div style={{ fontWeight: 700, fontSize: 15 }}>{p.name}</div>
            <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 10 }}>{p.key}</div>
            <p style={{ fontSize: 13, color: 'var(--text-secondary)', minHeight: 32 }}>{p.description}</p>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span className="badge">{p.status}</span>
              <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{p.members?.length || 0} membros</span>
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}

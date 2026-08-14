import { useEffect, useState, type FormEvent } from 'react';
import api from '../api/client';
import type { PublicUser, Team } from '../types';

export default function TeamsPage() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [users, setUsers] = useState<PublicUser[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ name: '', description: '', memberIds: [] as string[] });

  function load() {
    api.get<Team[]>('/teams').then((res) => setTeams(res.data));
  }

  useEffect(() => {
    load();
    api.get<PublicUser[]>('/users').then((res) => setUsers(res.data));
  }, []);

  async function createTeam(e: FormEvent) {
    e.preventDefault();
    await api.post('/teams', form);
    setForm({ name: '', description: '', memberIds: [] });
    setShowForm(false);
    load();
  }

  return (
    <div>
      <div className="page-header">
        <h1>Equipes</h1>
        <button className="btn btn-primary" onClick={() => setShowForm((s) => !s)}>
          + Nova equipe
        </button>
      </div>

      {showForm && (
        <form className="card" style={{ padding: 18, marginBottom: 20 }} onSubmit={createTeam}>
          <div className="form-row">
            <label className="label">Nome</label>
            <input className="input" required value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />
          </div>
          <div className="form-row">
            <label className="label">Descrição</label>
            <textarea className="textarea" value={form.description} onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))} />
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
        {teams.map((t) => (
          <div key={t.id} className="card" style={{ padding: 18 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
              <span style={{ width: 12, height: 12, borderRadius: '50%', background: t.color }} />
              <strong>{t.name}</strong>
            </div>
            <p style={{ fontSize: 13, color: 'var(--text-secondary)' }}>{t.description}</p>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 10 }}>
              {(t.members || []).map((m) => (
                <span key={m.id} className="badge">
                  {m.name}
                </span>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

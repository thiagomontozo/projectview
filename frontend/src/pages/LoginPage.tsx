import { useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';

export default function LoginPage() {
  const { login, adEnabled, user } = useAuth();
  const navigate = useNavigate();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [mode, setMode] = useState<'ad' | 'local'>('ad');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  if (user) {
    navigate('/', { replace: true });
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      await login(username, password, adEnabled ? mode : 'local');
      navigate('/', { replace: true });
    } catch (err: any) {
      setError(err?.response?.data?.message || 'Falha no login.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'linear-gradient(135deg, #184f95 0%, #2a78d6 100%)'
      }}
    >
      <div className="card" style={{ width: 380, padding: 32 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 6 }}>
          <div
            style={{
              width: 34,
              height: 34,
              borderRadius: 8,
              background: '#2a78d6',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: '#fff',
              fontWeight: 700
            }}
          >
            PV
          </div>
          <h1 style={{ fontSize: 20, margin: 0 }}>ProjectView</h1>
        </div>
        <p style={{ color: 'var(--text-secondary)', fontSize: 14, marginTop: 0, marginBottom: 22 }}>
          Faça login para acessar equipes, projetos e tarefas.
        </p>

        {adEnabled && (
          <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
            <button
              type="button"
              className="btn btn-sm"
              style={{ flex: 1, background: mode === 'ad' ? '#eef4fc' : undefined, borderColor: mode === 'ad' ? '#2a78d6' : undefined }}
              onClick={() => setMode('ad')}
            >
              Conta corporativa (AD)
            </button>
            <button
              type="button"
              className="btn btn-sm"
              style={{ flex: 1, background: mode === 'local' ? '#eef4fc' : undefined, borderColor: mode === 'local' ? '#2a78d6' : undefined }}
              onClick={() => setMode('local')}
            >
              Conta local
            </button>
          </div>
        )}

        <form onSubmit={handleSubmit}>
          <div className="form-row">
            <label className="label">Usuário</label>
            <input
              className="input"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="usuario ou usuario@dominio.com"
              autoFocus
              required
            />
          </div>
          <div className="form-row">
            <label className="label">Senha</label>
            <input
              className="input"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </div>
          {error && <div style={{ color: '#d03b3b', fontSize: 13, marginBottom: 12 }}>{error}</div>}
          <button type="submit" className="btn btn-primary" style={{ width: '100%', justifyContent: 'center' }} disabled={busy}>
            {busy ? 'Entrando...' : 'Entrar'}
          </button>
        </form>
      </div>
    </div>
  );
}

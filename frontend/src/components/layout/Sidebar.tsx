import { NavLink } from 'react-router-dom';

interface NavItem {
  to: string;
  label: string;
  end?: boolean;
  icon: string;
}

const links: NavItem[] = [
  { to: '/', label: 'Dashboard', end: true, icon: '📊' },
  { to: '/my-tasks', label: 'Minhas Tarefas', icon: '✅' },
  { to: '/projects', label: 'Projetos', icon: '📁' },
  { to: '/teams', label: 'Equipes', icon: '👥' },
  { to: '/resources', label: 'Alocação de Recursos', icon: '🧩' },
  { to: '/reports', label: 'Relatórios', icon: '📈' },
  { to: '/chat', label: 'Chat Interno', icon: '💬' }
];

export default function Sidebar() {
  return (
    <aside
      style={{
        background: '#101a2c',
        color: '#e8ecf3',
        padding: '18px 12px',
        display: 'flex',
        flexDirection: 'column',
        gap: 4
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '6px 10px 22px' }}>
        <div
          style={{
            width: 30,
            height: 30,
            borderRadius: 8,
            background: '#2a78d6',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontWeight: 700,
            fontSize: 13
          }}
        >
          PV
        </div>
        <strong style={{ fontSize: 15 }}>ProjectView</strong>
      </div>

      {links.map((link) => (
        <NavLink
          key={link.to}
          to={link.to}
          end={link.end}
          style={({ isActive }) => ({
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            padding: '9px 12px',
            borderRadius: 8,
            fontSize: 14,
            fontWeight: 500,
            color: isActive ? '#fff' : '#b7c1d6',
            background: isActive ? 'rgba(42,120,214,0.35)' : 'transparent'
          })}
        >
          <span>{link.icon}</span>
          {link.label}
        </NavLink>
      ))}
    </aside>
  );
}

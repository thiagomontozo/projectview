import { categorical, chrome } from '../../styles/theme.ts';
import type { ProjectProgressRow } from '../../types';

export default function ProjectProgress({ data }: { data: ProjectProgressRow[] }) {
  if (!data.length) {
    return <div style={{ color: chrome.muted, fontSize: 13, padding: '30px 0', textAlign: 'center' }}>Nenhum projeto ainda.</div>;
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      {data.map((row, i) => (
        <div key={row.project.id}>
          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, marginBottom: 4 }}>
            <span style={{ fontWeight: 600 }}>{row.project.name}</span>
            <span style={{ color: chrome.textSecondary }}>
              {row.done}/{row.total} ({row.percent}%)
            </span>
          </div>
          <div style={{ background: chrome.grid, borderRadius: 999, height: 8, overflow: 'hidden' }}>
            <div
              style={{
                width: `${row.percent}%`,
                height: '100%',
                background: categorical[i % categorical.length],
                borderRadius: 999,
                transition: 'width 0.3s'
              }}
            />
          </div>
        </div>
      ))}
    </div>
  );
}

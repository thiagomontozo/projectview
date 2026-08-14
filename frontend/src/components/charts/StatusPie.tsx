import { PieChart, Pie, Cell, Tooltip, Legend, ResponsiveContainer } from 'recharts';
import { categorical, chrome } from '../../styles/theme.ts';
import type { StatusBreakdownRow } from '../../types';

const labels: Record<string, string> = {
  backlog: 'Backlog',
  todo: 'A Fazer',
  in_progress: 'Em Progresso',
  review: 'Em Revisão',
  done: 'Concluído'
};

export default function StatusPie({ data }: { data: StatusBreakdownRow[] }) {
  const chartData = data.map((d) => ({ name: labels[d.status] || d.status, value: d.count }));

  if (chartData.every((d) => d.value === 0) || chartData.length === 0) {
    return <div style={{ color: chrome.muted, fontSize: 13, padding: '30px 0', textAlign: 'center' }}>Sem dados ainda.</div>;
  }

  return (
    <ResponsiveContainer width="100%" height={260}>
      <PieChart>
        <Pie data={chartData} dataKey="value" nameKey="name" innerRadius={60} outerRadius={95} paddingAngle={2}>
          {chartData.map((entry, index) => (
            <Cell key={entry.name} fill={categorical[index % categorical.length]} stroke={chrome.surface} strokeWidth={2} />
          ))}
        </Pie>
        <Tooltip contentStyle={{ borderRadius: 8, border: `1px solid ${chrome.grid}`, fontSize: 13 }} />
        <Legend verticalAlign="bottom" height={30} wrapperStyle={{ fontSize: 12, color: chrome.textSecondary }} />
      </PieChart>
    </ResponsiveContainer>
  );
}

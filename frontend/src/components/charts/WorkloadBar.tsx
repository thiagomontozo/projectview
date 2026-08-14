import { BarChart, Bar, XAxis, YAxis, Tooltip, CartesianGrid, ResponsiveContainer } from 'recharts';
import { categorical, chrome } from '../../styles/theme.ts';
import type { WorkloadChartRow } from '../../types';

export default function WorkloadBar({ data }: { data: WorkloadChartRow[] }) {
  if (!data.length) {
    return <div style={{ color: chrome.muted, fontSize: 13, padding: '30px 0', textAlign: 'center' }}>Sem dados ainda.</div>;
  }

  return (
    <ResponsiveContainer width="100%" height={280}>
      <BarChart data={data} layout="vertical" margin={{ left: 10, right: 20 }}>
        <CartesianGrid horizontal={false} stroke={chrome.grid} />
        <XAxis type="number" allowDecimals={false} tick={{ fontSize: 12, fill: chrome.muted }} stroke={chrome.baseline} />
        <YAxis type="category" dataKey="name" width={110} tick={{ fontSize: 12, fill: chrome.textSecondary }} stroke={chrome.baseline} />
        <Tooltip contentStyle={{ borderRadius: 8, border: `1px solid ${chrome.grid}`, fontSize: 13 }} cursor={{ fill: '#f2f2ef' }} />
        <Bar dataKey="count" name="Tarefas abertas" fill={categorical[0]} radius={[0, 4, 4, 0]} barSize={16} />
      </BarChart>
    </ResponsiveContainer>
  );
}

import { LineChart, Line, XAxis, YAxis, Tooltip, CartesianGrid, ResponsiveContainer } from 'recharts';
import { categorical, chrome } from '../../styles/theme.ts';
import type { CompletionTrendRow } from '../../types';

export default function CompletionTrend({ data }: { data: CompletionTrendRow[] }) {
  if (!data.length) {
    return (
      <div style={{ color: chrome.muted, fontSize: 13, padding: '30px 0', textAlign: 'center' }}>
        Sem tarefas concluídas nos últimos 30 dias.
      </div>
    );
  }

  return (
    <ResponsiveContainer width="100%" height={260}>
      <LineChart data={data} margin={{ left: 4, right: 20 }}>
        <CartesianGrid stroke={chrome.grid} vertical={false} />
        <XAxis dataKey="date" tick={{ fontSize: 11, fill: chrome.muted }} stroke={chrome.baseline} minTickGap={20} />
        <YAxis allowDecimals={false} tick={{ fontSize: 12, fill: chrome.muted }} stroke={chrome.baseline} />
        <Tooltip contentStyle={{ borderRadius: 8, border: `1px solid ${chrome.grid}`, fontSize: 13 }} />
        <Line type="monotone" dataKey="count" name="Tarefas concluídas" stroke={categorical[2]} strokeWidth={2} dot={{ r: 3 }} />
      </LineChart>
    </ResponsiveContainer>
  );
}

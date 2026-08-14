/**
 * Shared design tokens, following the dataviz skill's validated default
 * palette. Categorical hues are assigned in a fixed order and never
 * cycled/reassigned by filters.
 */
export const categorical: string[] = [
  '#2a78d6', // 1 blue
  '#eb6834', // 2 orange
  '#1baf7a', // 3 aqua
  '#eda100', // 4 yellow
  '#e87ba4', // 5 magenta
  '#008300', // 6 green
  '#4a3aa7', // 7 violet
  '#e34948' // 8 red
];

export const sequentialBlue: string[] = ['#cde2fb', '#9ec5f4', '#5598e7', '#2a78d6', '#184f95'];

export const status = {
  good: '#0ca30c',
  warning: '#fab219',
  serious: '#ec835a',
  critical: '#d03b3b'
} as const;

export const chrome = {
  surface: '#fcfcfb',
  page: '#f9f9f7',
  textPrimary: '#0b0b0b',
  textSecondary: '#52514e',
  muted: '#898781',
  grid: '#e1e0d9',
  baseline: '#c3c2b7',
  border: 'rgba(11,11,11,0.10)'
} as const;

export const columnColor: Record<string, string> = {
  backlog: '#898781',
  todo: categorical[0],
  in_progress: categorical[3],
  review: categorical[6],
  done: status.good
};

export const priorityColor: Record<string, string> = {
  low: '#898781',
  medium: categorical[0],
  high: categorical[1],
  urgent: status.critical
};

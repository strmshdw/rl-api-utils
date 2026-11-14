export type DateRangePreset = 'all' | 'today' | '7d' | '30d' | 'custom';

export const DEFAULT_CUSTOM_FROM = '2026-10-01';
export const DEFAULT_CUSTOM_TO = '2026-10-18';

export function buildDateRangeQueryParams(
  preset: DateRangePreset,
  customFrom: string,
  customTo: string,
): { from?: string; to?: string } {
  if (preset === 'all') {
    return {};
  }
  const now = new Date();
  if (preset === 'today') {
    const start = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    return { from: start.toISOString() };
  }
  if (preset === '7d') {
    const start = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
    return { from: start.toISOString() };
  }
  if (preset === '30d') {
    const start = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
    return { from: start.toISOString() };
  }
  if (preset === 'custom') {
    const res: { from?: string; to?: string } = {};
    if (customFrom) res.from = customFrom;
    if (customTo) res.to = customTo;
    return res;
  }
  return {};
}

export function getDateRangeLabel(
  preset: DateRangePreset,
  customFrom: string,
  customTo: string,
): string {
  switch (preset) {
    case 'today':
      return 'TODAY';
    case '7d':
      return 'LAST 7D';
    case '30d':
      return 'LAST 30D';
    case 'custom':
      return customFrom && customTo ? `${customFrom} → ${customTo}` : 'CUSTOM RANGE';
    default:
      return 'ALL TIME';
  }
}

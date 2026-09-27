export interface PlatformInfo {
  name: string;
  code: string;
  badgeClass: string;
}

export function parsePlatform(platformOrId: string): PlatformInfo {
  if (!platformOrId) {
    return { name: 'Unknown', code: 'unknown', badgeClass: 'bg-slate-800 text-slate-400 border-slate-700' };
  }
  const lower = platformOrId.toLowerCase();
  if (lower.startsWith('steam')) {
    return { name: 'Steam', code: 'steam', badgeClass: 'bg-blue-900/40 text-blue-300 border-blue-700/50' };
  }
  if (lower.startsWith('epic')) {
    return { name: 'Epic Games', code: 'epic', badgeClass: 'bg-zinc-800 text-zinc-200 border-zinc-700' };
  }
  if (lower.startsWith('psn') || lower.startsWith('playstation')) {
    return { name: 'PlayStation', code: 'psn', badgeClass: 'bg-indigo-900/40 text-indigo-300 border-indigo-700/50' };
  }
  if (lower.startsWith('xbox')) {
    return { name: 'Xbox', code: 'xbox', badgeClass: 'bg-emerald-900/40 text-emerald-300 border-emerald-700/50' };
  }
  if (lower.startsWith('bot') || lower.startsWith('unknown|0|0')) {
    return { name: 'AI Bot', code: 'bot', badgeClass: 'bg-amber-950/40 text-amber-300 border-amber-800/50' };
  }
  return { name: 'Unknown', code: 'unknown', badgeClass: 'bg-slate-800 text-slate-400 border-slate-700' };
}

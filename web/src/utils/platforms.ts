export type PlatformCode = 'steam' | 'epic' | 'psn' | 'xbox' | 'switch' | 'psynet' | 'bot' | 'unknown';

export interface PlatformInfo {
  name: string;
  shortLabel: string;
  code: PlatformCode;
  badgeClass: string;
}

export function parsePlatform(platformOrId: string | null | undefined, isBot = false): PlatformInfo {
  if (isBot) {
    return { name: 'AI Bot', shortLabel: 'BOT', code: 'bot', badgeClass: 'bg-amber-950/40 text-amber-300 border-amber-800/50' };
  }
  const trimmed = (platformOrId || '').trim();
  if (!trimmed) {
    return { name: 'Unknown', shortLabel: '—', code: 'unknown', badgeClass: 'bg-slate-800 text-slate-400 border-slate-700' };
  }
  const lower = trimmed.toLowerCase();
  if (lower.startsWith('steam')) {
    return { name: 'Steam', shortLabel: 'STEAM', code: 'steam', badgeClass: 'bg-blue-900/40 text-blue-300 border-blue-700/50' };
  }
  if (lower.startsWith('epic')) {
    return { name: 'Epic Games', shortLabel: 'EPIC', code: 'epic', badgeClass: 'bg-zinc-800 text-zinc-200 border-zinc-700' };
  }
  if (lower.startsWith('psn') || lower.startsWith('playstation') || lower.startsWith('ps4') || lower.startsWith('ps5')) {
    return { name: 'PlayStation', shortLabel: 'PLAYSTATION', code: 'psn', badgeClass: 'bg-indigo-900/40 text-indigo-300 border-indigo-700/50' };
  }
  if (lower.startsWith('xbox') || lower.startsWith('xbl')) {
    return { name: 'Xbox', shortLabel: 'XBOX', code: 'xbox', badgeClass: 'bg-emerald-900/40 text-emerald-300 border-emerald-700/50' };
  }
  if (lower.startsWith('switch') || lower.startsWith('nintendo') || lower.startsWith('nnid')) {
    return { name: 'Nintendo Switch', shortLabel: 'SWITCH', code: 'switch', badgeClass: 'bg-red-900/40 text-red-300 border-red-700/50' };
  }
  if (lower.startsWith('psynet')) {
    return { name: 'PsyNet', shortLabel: 'PSYNET', code: 'psynet', badgeClass: 'bg-cyan-900/40 text-cyan-300 border-cyan-700/50' };
  }
  if (lower.startsWith('bot') || lower.startsWith('unknown|0|0')) {
    return { name: 'AI Bot', shortLabel: 'BOT', code: 'bot', badgeClass: 'bg-amber-950/40 text-amber-300 border-amber-800/50' };
  }
  const rawToken = trimmed.split('|')[0].trim().toUpperCase();
  return {
    name: rawToken || 'Unknown',
    shortLabel: rawToken || '—',
    code: 'unknown',
    badgeClass: 'bg-slate-800 text-slate-400 border-slate-700',
  };
}

export function formatMMR(mmr: number | null | undefined): string {
  if (mmr == null || typeof mmr !== 'number' || isNaN(mmr) || mmr === 0) return '---';
  return mmr.toFixed(1);
}

export function formatMMRDelta(delta: number | null | undefined): string {
  if (delta == null || typeof delta !== 'number' || isNaN(delta) || delta === 0) return '±0.0';
  const sign = delta > 0 ? '+' : '';
  return `${sign}${delta.toFixed(1)}`;
}

export function formatWinRate(wins: number, losses: number): string {
  const total = wins + losses;
  if (total === 0) return '0.0%';
  return `${((wins / total) * 100).toFixed(1)}%`;
}

export function formatDuration(seconds: number): string {
  if (!seconds || seconds <= 0) return '0:00';
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return `${mins}:${secs.toString().padStart(2, '0')}`;
}

export function formatRelativeTime(dateStr: string): string {
  if (!dateStr) return '';
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);

  if (diffSec < 60) return 'just now';
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHours = Math.floor(diffMin / 60);
  if (diffHours < 24) return `${diffHours}h ago`;
  const diffDays = Math.floor(diffHours / 24);
  if (diffDays < 7) return `${diffDays}d ago`;
  return date.toLocaleDateString();
}

export function getRankTierColor(tier: number): string {
  if (tier >= 1 && tier <= 3) return 'text-amber-700 border-amber-800/60 bg-amber-950/30';
  if (tier >= 4 && tier <= 6) return 'text-slate-300 border-slate-500/60 bg-slate-900/40';
  if (tier >= 7 && tier <= 9) return 'text-yellow-400 border-yellow-500/60 bg-yellow-950/30';
  if (tier >= 10 && tier <= 12) return 'text-teal-300 border-teal-500/60 bg-teal-950/30';
  if (tier >= 13 && tier <= 15) return 'text-sky-400 border-sky-500/60 bg-sky-950/30';
  if (tier >= 16 && tier <= 18) return 'text-purple-400 border-purple-500/60 bg-purple-950/30';
  if (tier >= 19 && tier <= 21) return 'text-rose-400 border-rose-500/60 bg-rose-950/30';
  if (tier === 22) return 'text-white border-cyan-400/80 bg-gradient-to-r from-purple-900/40 via-cyan-900/40 to-slate-900/40 shadow-[0_0_12px_rgba(56,189,248,0.4)]';
  return 'text-slate-400 border-slate-700 bg-slate-900/30';
}

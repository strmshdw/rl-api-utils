// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { H2HBadge } from './H2HBadge';
import { PlayerMatchup } from '../../types/api';

describe('H2HBadge Component', () => {
  const sampleMatchup: PlayerMatchup = {
    player_id: 'Steam|12345|0',
    playlist_id: 11,
    wins_as_teammate: 8,
    losses_as_teammate: 2,
    wins_as_opponent: 3,
    losses_as_opponent: 7,
    total_matches: 20,
    last_played_at: '2026-09-26T00:00:00Z',
  };

  const clean = (s: string) => s.replace(/<!--.*?-->/g, '');

  it('renders teammate record when isTeammate is true', () => {
    const html = clean(renderToString(<H2HBadge matchup={sampleMatchup} isTeammate={true} />));
    expect(html).toContain('8W-2L');
    expect(html).toContain('(80%)');
    expect(html).toContain('text-emerald-400');
  });

  it('renders opponent record when isTeammate is false', () => {
    const html = clean(renderToString(<H2HBadge matchup={sampleMatchup} isTeammate={false} />));
    expect(html).toContain('3W-7L');
    expect(html).toContain('(30%)');
    expect(html).toContain('text-rose-400');
  });

  it('renders neutral slate styling for 40-59% win rate', () => {
    const evenMatchup: PlayerMatchup = {
      ...sampleMatchup,
      wins_as_teammate: 5,
      losses_as_teammate: 5,
    };
    const html = clean(renderToString(<H2HBadge matchup={evenMatchup} isTeammate={true} />));
    expect(html).toContain('5W-5L');
    expect(html).toContain('(50%)');
    expect(html).toContain('text-slate-300');
  });

  it('renders -- when matchup is undefined or total_matches is 0', () => {
    expect(renderToString(<H2HBadge matchup={undefined} isTeammate={true} />)).toContain('--');
    expect(renderToString(<H2HBadge matchup={{ ...sampleMatchup, total_matches: 0 }} isTeammate={true} />)).toContain('--');
  });

  it('renders 0-0 when total matches for the specific category is 0', () => {
    const zeroTeammate: PlayerMatchup = {
      ...sampleMatchup,
      wins_as_teammate: 0,
      losses_as_teammate: 0,
    };
    expect(renderToString(<H2HBadge matchup={zeroTeammate} isTeammate={true} />)).toContain('0-0');
  });
});

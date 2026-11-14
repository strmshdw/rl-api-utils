// ============================================================================
// Canonical Rocket League Arena Names & Playlist Mode Filter Helpers
// ============================================================================

const ARENA_NAMES: Record<string, string> = {
  stadium_p: 'DFH Stadium',
  stadium_day_p: 'DFH Stadium (Day)',
  stadium_winter_p: 'DFH Stadium (Snowy)',
  stadium_foggy_p: 'DFH Stadium (Stormy)',
  stadium_race_day_p: 'DFH Stadium (Circuit)',
  stadium_10a_p: 'DFH Stadium (10th Anniversary)',
  eurostadium_p: 'Mannfield',
  eurostadium_night_p: 'Mannfield (Night)',
  eurostadium_rainy_p: 'Mannfield (Stormy)',
  eurostadium_snownight_p: 'Mannfield (Snowy)',
  eurostadium_dusk_p: 'Mannfield (Dusk)',
  park_p: 'Beckwith Park',
  park_night_p: 'Beckwith Park (Midnight)',
  park_rainy_p: 'Beckwith Park (Stormy)',
  park_snowy_p: 'Beckwith Park (Snowy)',
  utopiastadium_p: 'Utopia Coliseum',
  utopiastadium_dusk_p: 'Utopia Coliseum (Dusk)',
  utopiastadium_snow_p: 'Utopia Coliseum (Snowy)',
  utopiastadium_lux_p: 'Utopia Coliseum (Gilded)',
  uf_day_p: 'Urban Future (Day)',
  trainstation_p: 'Urban Central',
  trainstation_night_p: 'Urban Central (Night)',
  trainstation_dawn_p: 'Urban Central (Dawn)',
  trainstation_spooky_p: 'Urban Central (Haunted)',
  wasteland_p: 'Wasteland',
  wasteland_s_p: 'Wasteland',
  wasteland_night_p: 'Wasteland (Night)',
  wasteland_night_s_p: 'Wasteland (Night)',
  wasteland_grit_p: 'Wasteland (Pitched)',
  neotokyo_p: 'Neo Tokyo',
  neotokyo_standard_p: 'Neo Tokyo',
  neotokyo_toon_p: 'Neo Tokyo (Comic)',
  neotokyo_hax_p: 'Neo Tokyo (Hacked)',
  neotokyo_arcade_p: 'Neo Tokyo (Arcade)',
  arc_p: 'Starbase ARC',
  arc_standard_p: 'Starbase ARC',
  arc_darc_p: 'Starbase ARC (Aftermath)',
  underwater_p: 'AquaDome',
  underwater_grs_p: 'AquaDome (Salty Shallows)',
  farm_p: 'Farmstead',
  farm_night_p: 'Farmstead (Night)',
  farm_upsidedown_p: 'Farmstead (The Upside Down)',
  farm_grs_p: 'Farmstead (Pitched)',
  chn_stadium_p: 'Forbidden Temple',
  chn_stadium_day_p: 'Forbidden Temple (Day)',
  fni_stadium_p: 'Forbidden Temple (Fire & Ice)',
  cs_p: 'Champions Field',
  cs_day_p: 'Champions Field (Day)',
  cs_hw_p: 'Rivals Arena',
  cs_ralph_p: 'Champions Field (NFL)',
  beach_p: 'Salty Shores',
  beach_night_p: 'Salty Shores (Night)',
  beachblast_p: 'Salty Shores (Salty Fest)',
  music_p: 'Neon Fields',
  outlaw_p: 'Deadeye Canyon',
  outlaw_oasis_p: 'Deadeye Canyon (Oasis)',
  ff_dusk_p: 'Estadio Vida',
  favela_p: 'Sovereign Heights',
  favela_night_p: 'Sovereign Heights (Night)',
  street_p: 'Sovereign Heights',
  woods_p: 'Drift Woods',
  woods_night_p: 'Drift Woods (Night)',
  mall_day_p: 'Boostfield Mall',
  hoopsstadium_p: 'Dunk House',
  hoopsstreet_p: 'The Block',
  shattershot_p: 'Core 707',
  throwbackstadium_p: 'Throwback Stadium',
  koi_p: 'Mannfield',
};

const UPPER_ACRONYMS = new Set(['DFH', 'ARC', 'RL', 'II', 'III', 'IV', 'NFL']);

/**
 * Converts raw UE3 Rocket League map filenames (e.g. "stadium_10a_p", "UF_Day_P")
 * into canonical human-readable arena names, with clean title-case fallback.
 */
export function formatArenaName(rawMap?: string | null): string {
  if (!rawMap) return '—';
  const trimmed = rawMap.trim();
  if (!trimmed) return '—';

  const mapped = ARENA_NAMES[trimmed.toLowerCase()];
  if (mapped) return mapped;

  // Preserve already human-readable names without UE3 underscore suffixes
  if (!trimmed.includes('_')) return trimmed;

  const stripped = trimmed.replace(/(?:_s)?_[ps]$/i, '');
  const words = stripped
    .split(/_+/)
    .map((w) => w.trim())
    .filter(Boolean)
    .map((w) => {
      const upper = w.toUpperCase();
      if (UPPER_ACRONYMS.has(upper)) return upper;
      return w.charAt(0).toUpperCase() + w.slice(1).toLowerCase();
    });

  return words.length > 0 ? words.join(' ') : trimmed;
}

const EXTRA_MODE_IDS = new Set([27, 28, 29, 30, 34]);
const CASUAL_MODE_IDS = new Set([1, 2, 3, 4, 15, 16, 17, 18, 23]);
const PRIVATE_MODE_IDS = new Set([6, 7, 8, 22, 24]);

/** Evaluates whether a numeric playlist ID belongs to the selected UI filter key. */
export function matchesPlaylistFilter(playlistId: number, filter: string): boolean {
  const f = (filter || '').trim().toLowerCase();
  if (!f || f === 'all' || f === '0') return true;
  switch (f) {
    case '2v2':
    case 'doubles':
    case 'ranked_2v2':
    case '11':
      return playlistId === 11;
    case '3v3':
    case 'standard':
    case 'ranked_3v3':
      return playlistId === 13 || playlistId === 12;
    case '13':
      return playlistId === 13;
    case '12':
      return playlistId === 12;
    case '1v1':
    case 'duel':
    case 'ranked_1v1':
    case '10':
      return playlistId === 10;
    case 'extra':
      return EXTRA_MODE_IDS.has(playlistId);
    case 'hoops':
    case '27':
      return playlistId === 27;
    case 'rumble':
    case '28':
      return playlistId === 28;
    case 'dropshot':
    case '29':
      return playlistId === 29;
    case 'snowday':
    case 'snow_day':
    case '30':
      return playlistId === 30;
    case 'tournament':
    case 'tournaments':
    case '34':
      return playlistId === 34;
    case 'casual':
    case 'unranked':
      return CASUAL_MODE_IDS.has(playlistId);
    case 'private':
    case 'custom':
      return PRIVATE_MODE_IDS.has(playlistId);
    default: {
      const parts = f.split(',').map((s) => Number(s.trim()));
      return parts.some((n) => Number.isInteger(n) && n > 0 && n === playlistId);
    }
  }
}

/** Normalizes UI mode filter keys to backend query parameter values. */
export function playlistFilterToParam(filter: string): string {
  const f = (filter || '').trim().toLowerCase();
  if (!f || f === 'all' || f === '0') return '';
  switch (f) {
    case '2v2':
    case 'doubles':
    case 'ranked_2v2':
    case '11':
      return '11';
    case '3v3':
    case 'standard':
    case 'ranked_3v3':
    case '13':
      return '13';
    case '1v1':
    case 'duel':
    case 'ranked_1v1':
    case '10':
      return '10';
    case 'hoops':
    case '27':
      return '27';
    case 'rumble':
    case '28':
      return '28';
    case 'dropshot':
    case '29':
      return '29';
    case 'snowday':
    case 'snow_day':
    case '30':
      return '30';
    case 'tournament':
    case 'tournaments':
    case '34':
      return '34';
    case 'extra':
      return 'extra';
    case 'casual':
    case 'unranked':
      return 'casual';
    case 'private':
    case 'custom':
      return 'private';
    default:
      return f;
  }
}

/** Maps a numeric Rocket League playlist ID to its canonical dashboard mode key. */
export function modeKeyFromPlaylistId(playlistId: number): string {
  if (playlistId === 11) return '11';
  if (playlistId === 13 || playlistId === 12) return '13';
  if (playlistId === 10) return '10';
  if (playlistId === 27) return '27';
  if (playlistId === 28) return '28';
  if (playlistId === 29) return '29';
  if (playlistId === 30) return '30';
  if (playlistId === 34) return '34';
  if (CASUAL_MODE_IDS.has(playlistId)) return 'casual';
  if (PRIVATE_MODE_IDS.has(playlistId)) return 'private';
  return playlistId > 0 ? String(playlistId) : '11';
}

export interface PositioningRoleLabels {
  showRotation: boolean;
  forwardLabel: string;
  backLabel: string;
}

const DUEL_ROTATION_KEYS = new Set(['1', '10', '1v1', 'duel']);
const DOUBLES_ROTATION_KEYS = new Set(['2', '11', '27', '43', '65', '67', '2v2', 'hoops', 'doubles']);
const STANDARD_ROTATION_KEYS = new Set([
  '3',
  '12',
  '13',
  '28',
  '29',
  '30',
  '34',
  '3v3',
  'standard',
  'rumble',
  'dropshot',
  'snowday',
  'tournament',
]);

/** Returns playlist-accurate positioning rotation column headers (or hides them in 1v1). */
export function positioningRoleLabels(playlistId?: number | string | null): PositioningRoleLabels {
  if (playlistId == null) {
    return { showRotation: true, forwardLabel: 'MOST FWD', backLabel: 'MOST BACK' };
  }
  const key = String(playlistId).trim().toLowerCase();
  if (DUEL_ROTATION_KEYS.has(key)) {
    return { showRotation: false, forwardLabel: 'MOST FWD', backLabel: 'MOST BACK' };
  }
  if (DOUBLES_ROTATION_KEYS.has(key)) {
    return { showRotation: true, forwardLabel: '1ST MAN', backLabel: '2ND MAN' };
  }
  if (STANDARD_ROTATION_KEYS.has(key)) {
    return { showRotation: true, forwardLabel: '1ST MAN', backLabel: '3RD MAN' };
  }
  return { showRotation: true, forwardLabel: 'MOST FWD', backLabel: 'MOST BACK' };
}

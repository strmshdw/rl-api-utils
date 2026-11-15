// Barrel re-export of broadcast display helpers, session KPI summarization,
// live threat badges, and honest role-stats telemetry helpers.
export {
  CELL_BG,
  OUTCOME_SLAB,
  TEAM_INK,
  TEAM_SLAB_BORDER,
  compactRank,
  computeDynamicRelationshipTag,
  deltaInk,
  findMvp,
  formatAgo,
  formatArenaName,
  formatElapsed,
  formatMatchClock,
  formatMmr,
  formatPct,
  formatRecord,
  formatShortDate,
  formatSigned,
  localTeamOf,
  matchesPlaylistFilter,
  modeKeyFromPlaylistId,
  onlineIdOf,
  outcomeOf,
  platformLabel,
  playlistFilterToParam,
  playlistLabels,
  positioningRoleLabels,
  recordCells,
  recordFill,
  recordInk,
  rosterLine,
  shortRank,
  teamName,
  trackerUrl,
  zeroInk,
  type CellKind,
  type Outcome,
  type PlaylistLabels,
  type PositioningRoleLabels,
  type RelationshipTagInfo,
  type RelationshipTagOptions,
} from './broadcastLabels';

export {
  chronological,
  filterSessionByPlaylist,
  summarizeSession,
  summarizeSessionPlaylists,
  type SessionKpi,
  type SessionPlaylistBreakdownItem,
  type SessionSummary,
} from './sessionSummary';

export {
  computeLiveThreatBadges,
  soccarEquivalentTier,
  type LiveThreatBadge,
} from './threatBadges';

export {
  DEFAULT_STAMPING_CONFIG,
  STAMPING_BOUNDS,
  isStampingConfigPayload,
  normalizeStampingConfig,
  type NumericStampingBounds,
  type StampingConfig,
} from './stampingConfig';

export {
  emptyRoleStats,
  hasSyncedRoleTelemetry,
} from './roleStats';

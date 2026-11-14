// Barrel re-export of broadcast display helpers, session KPI summarization,
// live threat badges, and honest role-stats telemetry helpers.
export {
  CELL_BG,
  OUTCOME_SLAB,
  TEAM_INK,
  TEAM_SLAB_BORDER,
  computeDynamicRelationshipTag,
  deltaInk,
  findMvp,
  formatAgo,
  formatElapsed,
  formatMatchClock,
  formatMmr,
  formatPct,
  formatRecord,
  formatShortDate,
  formatSigned,
  localTeamOf,
  onlineIdOf,
  outcomeOf,
  platformLabel,
  playlistLabels,
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
  type RelationshipTagInfo,
} from './broadcastLabels';

export {
  chronological,
  summarizeSession,
  type SessionKpi,
  type SessionSummary,
} from './sessionSummary';

export {
  computeLiveThreatBadges,
  type LiveThreatBadge,
} from './threatBadges';

export {
  emptyRoleStats,
  hasSyncedRoleTelemetry,
} from './roleStats';

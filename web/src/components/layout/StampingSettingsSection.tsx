import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useDashboard } from '../../context/DashboardContext';
import type { StampingConfig } from '../../types/api';
import { STAMPING_BOUNDS, type NumericStampingBounds } from '../../utils/broadcast';
import { isDemoMode } from '../../utils/sampleData';
import { SectionLabel } from '../common/ui';

const TIER_OPTIONS: ReadonlyArray<{ value: number; label: string }> = [
  { value: 1, label: 'Tier 1 · Bronze I+' },
  { value: 4, label: 'Tier 4 · Silver I+' },
  { value: 7, label: 'Tier 7 · Gold I+' },
  { value: 10, label: 'Tier 10 · Platinum I+' },
  { value: 12, label: 'Tier 12 · Platinum III+ (Default)' },
  { value: 13, label: 'Tier 13 · Diamond I+' },
  { value: 15, label: 'Tier 15 · Diamond III+' },
  { value: 16, label: 'Tier 16 · Champion I+' },
  { value: 18, label: 'Tier 18 · Champion III+' },
  { value: 19, label: 'Tier 19 · Grand Champion I+' },
  { value: 22, label: 'Tier 22 · Supersonic Legend' },
];

interface ThresholdNumberFieldProps {
  id: string;
  name: keyof StampingConfig;
  label: string;
  hint: string;
  value: number;
  bounds: NumericStampingBounds;
  disabled?: boolean;
  pairedRow?: boolean;
  onCommit: (key: keyof StampingConfig, nextValue: number) => void;
  onAnnounceError: (msg: string) => void;
}

const ThresholdNumberField: React.FC<ThresholdNumberFieldProps> = ({
  id,
  name,
  label,
  hint,
  value,
  bounds,
  disabled = false,
  pairedRow = false,
  onCommit,
  onAnnounceError,
}) => {
  const [draft, setDraft] = useState<string>(() => String(value));
  const [prevValue, setPrevValue] = useState<number>(value);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const latestDraftRef = useRef<string>(draft);
  const latestValueRef = useRef<number>(value);

  if (prevValue !== value) {
    setPrevValue(value);
    setDraft(String(value));
    setError(null);
  }

  latestDraftRef.current = draft;
  latestValueRef.current = value;

  const parseValid = useCallback(
    (raw: string): number | null => {
      const trimmed = raw.trim();
      if (!trimmed) return null;
      const num = Number(trimmed);
      if (!Number.isFinite(num) || num < bounds.min || num > bounds.max) return null;
      return bounds.step >= 1 ? Math.round(num) : Math.round(num * 100) / 100;
    },
    [bounds.max, bounds.min, bounds.step],
  );

  const commitCurrentDraft = useCallback(() => {
    const parsed = parseValid(latestDraftRef.current);
    if (parsed === null) {
      const msg = `${label}: Enter a value from ${bounds.min} to ${bounds.max}.`;
      setError(`Enter a value from ${bounds.min} to ${bounds.max}.`);
      onAnnounceError(msg);
      return;
    }
    setError(null);
    setDraft(String(parsed));
    if (parsed !== latestValueRef.current) {
      latestValueRef.current = parsed;
      onCommit(name, parsed);
    }
  }, [bounds.max, bounds.min, label, name, onAnnounceError, onCommit, parseValid]);

  useEffect(() => {
    const el = inputRef.current;
    if (!el) return;
    const onNativeBlur = () => commitCurrentDraft();
    el.addEventListener('blur', onNativeBlur);
    return () => el.removeEventListener('blur', onNativeBlur);
  }, [commitCurrentDraft]);

  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  const describedBy = error ? `${hintId} ${errorId}` : hintId;

  return (
    <div
      data-testid={`threshold-field-${name}`}
      className={
        pairedRow
          ? 'row-span-3 grid grid-rows-subgrid gap-y-1 min-w-0'
          : 'flex flex-col gap-1 min-w-0'
      }
    >
      <div className="flex flex-col justify-between gap-1 min-w-0">
        <label
          htmlFor={id}
          className="t-font-display font-bold text-xs uppercase tracking-wider t-text-strong leading-tight break-words"
        >
          {label}
        </label>
        <span id={hintId} className="text-[11px] text-muted leading-tight break-words">
          {hint} ({bounds.min}–{bounds.max})
        </span>
      </div>
      <input
        ref={inputRef}
        type="number"
        id={id}
        name={name}
        min={bounds.min}
        max={bounds.max}
        step={bounds.step}
        disabled={disabled}
        value={draft}
        aria-invalid={error ? 'true' : 'false'}
        aria-describedby={describedBy}
        data-error={errorId}
        onChange={(e) => {
          const next = e.target.value;
          setDraft(next);
          if (error && parseValid(next) !== null) {
            setError(null);
          }
        }}
        onBlur={commitCurrentDraft}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
            e.preventDefault();
            commitCurrentDraft();
          }
        }}
        className={`w-full min-h-[38px] px-2.5 py-1.5 t-bg-page t-text-strong border text-xs t-font-mono font-bold t-radius focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-ink disabled:opacity-50 ${
          error ? 'border-loss' : 'border-outline'
        }`}
      />
      {pairedRow ? (
        <div className="min-h-0">
          {error && (
            <div id={errorId} className="text-[11px] font-bold text-loss flex items-center gap-1">
              <span role="img" aria-label="Error">
                ⚠
              </span>
              <span>{error}</span>
            </div>
          )}
        </div>
      ) : (
        error && (
          <div id={errorId} className="text-[11px] font-bold text-loss flex items-center gap-1">
            <span role="img" aria-label="Error">
              ⚠
            </span>
            <span>{error}</span>
          </div>
        )
      )}
    </div>
  );
};

interface StampToggleRowProps {
  id: string;
  name: keyof StampingConfig;
  label: string;
  description: string;
  checked: boolean;
  onToggle: (key: keyof StampingConfig, next: boolean) => void;
}

const StampToggleRow: React.FC<StampToggleRowProps> = ({
  id,
  name,
  label,
  description,
  checked,
  onToggle,
}) => {
  const descId = `${id}-desc`;
  return (
    <div className="flex items-start gap-2.5 py-1">
      <input
        type="checkbox"
        id={id}
        name={name}
        checked={checked}
        aria-invalid="false"
        aria-describedby={descId}
        onChange={(e) => onToggle(name, e.target.checked)}
        style={{ accentColor: 'var(--blue-ink, #2563eb)' }}
        className="mt-0.5 h-4 w-4 shrink-0 cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-ink"
      />
      <div className="flex flex-col gap-0.5 min-w-0">
        <label
          htmlFor={id}
          className="t-font-display font-bold text-xs uppercase tracking-wider t-text-strong cursor-pointer"
        >
          {label}
        </label>
        <span id={descId} className="text-[11px] text-muted leading-snug">
          {description}
        </span>
      </div>
    </div>
  );
};

export const StampingSettingsSection: React.FC = () => {
  const {
    stampingConfig,
    stampingSyncState,
    refreshStampingConfig,
    updateStampingConfig,
    resetStampingConfig,
  } = useDashboard();
  const [liveAnnouncement, setLiveAnnouncement] = useState<string>('');

  useEffect(() => {
    if (!isDemoMode()) {
      void refreshStampingConfig();
    }
  }, [refreshStampingConfig]);

  const handleToggle = useCallback(
    (key: keyof StampingConfig, next: boolean) => {
      void updateStampingConfig({ [key]: next } as Partial<StampingConfig>);
    },
    [updateStampingConfig],
  );

  const handleCommitNumber = useCallback(
    (key: keyof StampingConfig, nextValue: number) => {
      void updateStampingConfig({ [key]: nextValue } as Partial<StampingConfig>);
    },
    [updateStampingConfig],
  );

  const statusText =
    stampingSyncState === 'saving'
      ? 'SAVING…'
      : stampingSyncState === 'saved'
      ? 'SAVED ✓'
      : stampingSyncState === 'reset'
      ? 'DEFAULTS RESTORED ✓'
      : stampingSyncState === 'error'
      ? '⚠ SAVE FAILED'
      : 'MULTI-SIGNAL ENGINE';

  return (
    <section className="flex flex-col gap-3.5 pt-2 border-t border-line">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div id="stamping-settings-heading">
          <SectionLabel>02 · STAMPING &amp; THREAT THRESHOLDS</SectionLabel>
        </div>
        <div className="flex items-center gap-2">
          <span
            role="status"
            data-testid="stamping-sync-status"
            className={`t-font-mono text-[11px] font-bold uppercase tracking-wider ${
              stampingSyncState === 'error' ? 'text-loss' : 'text-muted'
            }`}
          >
            {statusText}
          </span>
          <button
            type="button"
            data-testid="reset-stamping-defaults-btn"
            onClick={() => {
              void resetStampingConfig();
            }}
            className="px-2.5 py-1 t-bg-page border border-outline hover:border-ink t-font-display font-bold text-xs uppercase tracking-wider t-text-strong t-radius cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-ink"
          >
            RESET DEFAULTS
          </button>
        </div>
      </div>

      <div aria-live="polite" className="sr-only">
        {liveAnnouncement}
      </div>

      <form
        aria-labelledby="stamping-settings-heading"
        method="POST"
        action="/api/stamping/config"
        onSubmit={(e) => e.preventDefault()}
        className="grid grid-cols-1 md:grid-cols-2 gap-3"
      >
        {/* Group 1: Fresh Alt / Smurf Detection */}
        <fieldset className="surface-plaque border border-line p-3 flex flex-col gap-2.5 t-radius m-0 min-w-0">
          <legend className="px-1 t-font-display font-bold text-xs uppercase tracking-wider text-muted">
            ALT &amp; UNCERTAINTY SEAL (LIKELY ALT)
          </legend>
          <StampToggleRow
            id="stamping-alt-detection-enabled"
            name="alt_detection_enabled"
            label="ENABLE ALT / SMURF UNCERTAINTY SEAL"
            description="Flags fresh opponent accounts combining elevated PsyNet Sigma with low account match volume."
            checked={stampingConfig.alt_detection_enabled}
            onToggle={handleToggle}
          />

          <div className="flex flex-col gap-1">
            <label
              htmlFor="stamping-alt-min-tier"
              className="t-font-display font-bold text-xs uppercase tracking-wider t-text-strong"
            >
              MINIMUM ACTIVE LOBBY TIER
            </label>
            <span id="stamping-alt-min-tier-hint" className="text-[11px] text-muted leading-tight">
              Suppress alt seals in lower ranks where high Sigma is normal for new players.
            </span>
            <select
              id="stamping-alt-min-tier"
              name="alt_min_tier"
              disabled={!stampingConfig.alt_detection_enabled}
              value={stampingConfig.alt_min_tier}
              aria-invalid="false"
              aria-describedby="stamping-alt-min-tier-hint"
              onChange={(e) => handleCommitNumber('alt_min_tier', Number(e.target.value))}
              className="w-full min-h-[38px] px-2.5 py-1.5 t-bg-page t-text-strong border border-outline text-xs font-semibold t-radius cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-ink disabled:opacity-50"
            >
              {!TIER_OPTIONS.some((o) => o.value === stampingConfig.alt_min_tier) && (
                <option value={stampingConfig.alt_min_tier}>
                  Tier {stampingConfig.alt_min_tier}
                </option>
              )}
              {TIER_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </div>

          <div className="grid grid-cols-2 gap-x-2.5">
            <ThresholdNumberField
              id="stamping-alt-high-sigma"
              name="alt_high_sigma"
              label="FRESH ALT SIGMA (σ)"
              hint="High uncertainty floor"
              value={stampingConfig.alt_high_sigma}
              bounds={STAMPING_BOUNDS.alt_high_sigma}
              disabled={!stampingConfig.alt_detection_enabled}
              pairedRow
              onCommit={handleCommitNumber}
              onAnnounceError={setLiveAnnouncement}
            />
            <ThresholdNumberField
              id="stamping-alt-high-max-games"
              name="alt_high_sigma_max_games"
              label="FRESH ALT MAX GAMES"
              hint="Account match cap"
              value={stampingConfig.alt_high_sigma_max_games}
              bounds={STAMPING_BOUNDS.alt_high_sigma_max_games}
              disabled={!stampingConfig.alt_detection_enabled}
              pairedRow
              onCommit={handleCommitNumber}
              onAnnounceError={setLiveAnnouncement}
            />
          </div>

          <div className="grid grid-cols-2 gap-x-2.5">
            <ThresholdNumberField
              id="stamping-alt-streak-sigma"
              name="alt_streak_sigma"
              label="STREAK ALT SIGMA (σ)"
              hint="Elevated σ + streak"
              value={stampingConfig.alt_streak_sigma}
              bounds={STAMPING_BOUNDS.alt_streak_sigma}
              disabled={!stampingConfig.alt_detection_enabled}
              pairedRow
              onCommit={handleCommitNumber}
              onAnnounceError={setLiveAnnouncement}
            />
            <ThresholdNumberField
              id="stamping-alt-streak-max-games"
              name="alt_streak_max_games"
              label="STREAK ALT MAX GAMES"
              hint="Streak account cap"
              value={stampingConfig.alt_streak_max_games}
              bounds={STAMPING_BOUNDS.alt_streak_max_games}
              disabled={!stampingConfig.alt_detection_enabled}
              pairedRow
              onCommit={handleCommitNumber}
              onAnnounceError={setLiveAnnouncement}
            />
          </div>

          <ThresholdNumberField
            id="stamping-alt-min-win-streak"
            name="alt_min_win_streak"
            label="STREAK ALT MIN WINS"
            hint="Consecutive wins required for streak-alt trigger"
            value={stampingConfig.alt_min_win_streak}
            bounds={STAMPING_BOUNDS.alt_min_win_streak}
            disabled={!stampingConfig.alt_detection_enabled}
            onCommit={handleCommitNumber}
            onAnnounceError={setLiveAnnouncement}
          />
        </fieldset>

        {/* Group 2: Calibrated Cross-Playlist Off-Mode Peaks */}
        <fieldset className="surface-plaque border border-line p-3 flex flex-col gap-2.5 t-radius m-0 min-w-0">
          <legend className="px-1 t-font-display font-bold text-xs uppercase tracking-wider text-muted">
            CROSS-PLAYLIST OFF-MODE PEAKS
          </legend>
          <StampToggleRow
            id="stamping-show-off-mode-peak"
            name="show_off_mode_peak"
            label="SHOW OPPONENT OFF-MODE PEAK"
            description="Surfaces calibrated peaks in relevant playlists (1v1 compression normalized; Extra Modes isolated from Soccar)."
            checked={stampingConfig.show_off_mode_peak}
            onToggle={handleToggle}
          />
          <StampToggleRow
            id="stamping-show-teammate-peak"
            name="show_teammate_off_mode_peak"
            label="SHOW TEAMMATE OFF-MODE PEAK"
            description="Optionally display neutral TEAMMATE PEAK badges on allied cards."
            checked={stampingConfig.show_teammate_off_mode_peak}
            onToggle={handleToggle}
          />

          <ThresholdNumberField
            id="stamping-off-mode-min-games"
            name="off_mode_min_games"
            label="CALIBRATION MIN GAMES"
            hint="Minimum games played in the off-playlist"
            value={stampingConfig.off_mode_min_games}
            bounds={STAMPING_BOUNDS.off_mode_min_games}
            disabled={!stampingConfig.show_off_mode_peak && !stampingConfig.show_teammate_off_mode_peak}
            onCommit={handleCommitNumber}
            onAnnounceError={setLiveAnnouncement}
          />

          <div className="grid grid-cols-2 gap-x-2.5">
            <ThresholdNumberField
              id="stamping-off-mode-self-tier-diff"
              name="off_mode_self_tier_diff"
              label="GAP VS OWN ACTIVE TIER"
              hint="Normalized tiers above own active rank"
              value={stampingConfig.off_mode_self_tier_diff}
              bounds={STAMPING_BOUNDS.off_mode_self_tier_diff}
              disabled={!stampingConfig.show_off_mode_peak && !stampingConfig.show_teammate_off_mode_peak}
              pairedRow
              onCommit={handleCommitNumber}
              onAnnounceError={setLiveAnnouncement}
            />
            <ThresholdNumberField
              id="stamping-off-mode-vs-you-tier-diff"
              name="off_mode_vs_you_tier_diff"
              label="GAP VS YOUR SKILL CEILING"
              hint="Tiers above your calibrated Soccar peak"
              value={stampingConfig.off_mode_vs_you_tier_diff}
              bounds={STAMPING_BOUNDS.off_mode_vs_you_tier_diff}
              disabled={!stampingConfig.show_off_mode_peak}
              pairedRow
              onCommit={handleCommitNumber}
              onAnnounceError={setLiveAnnouncement}
            />
          </div>
        </fieldset>

        {/* Group 3: Active Lobby MMR & Win Streaks */}
        <fieldset className="surface-plaque border border-line p-3 flex flex-col gap-2.5 t-radius m-0 min-w-0">
          <legend className="px-1 t-font-display font-bold text-xs uppercase tracking-wider text-muted">
            LOBBY MMR &amp; MOMENTUM STAMPS
          </legend>
          <div className="grid grid-cols-2 gap-x-2.5">
            <ThresholdNumberField
              id="stamping-higher-mmr-diff"
              name="higher_mmr_diff"
              label="HIGHER MMR (+VS YOU)"
              hint="Active MMR lead over you"
              value={stampingConfig.higher_mmr_diff}
              bounds={STAMPING_BOUNDS.higher_mmr_diff}
              pairedRow
              onCommit={handleCommitNumber}
              onAnnounceError={setLiveAnnouncement}
            />
            <ThresholdNumberField
              id="stamping-carry-mmr-diff"
              name="carry_mmr_diff"
              label="PARTY CARRY (+VS MATE)"
              hint="Same-team MMR gap"
              value={stampingConfig.carry_mmr_diff}
              bounds={STAMPING_BOUNDS.carry_mmr_diff}
              pairedRow
              onCommit={handleCommitNumber}
              onAnnounceError={setLiveAnnouncement}
            />
          </div>

          <StampToggleRow
            id="stamping-show-hot-streak"
            name="show_hot_streak"
            label="SHOW HOT STREAK STAMP"
            description="Highlights active-playlist win streaks on teammates and opponents."
            checked={stampingConfig.show_hot_streak}
            onToggle={handleToggle}
          />
          <ThresholdNumberField
            id="stamping-hot-streak-min-wins"
            name="hot_streak_min_wins"
            label="HOT STREAK MIN WINS"
            hint="Consecutive playlist wins required"
            value={stampingConfig.hot_streak_min_wins}
            bounds={STAMPING_BOUNDS.hot_streak_min_wins}
            disabled={!stampingConfig.show_hot_streak}
            onCommit={handleCommitNumber}
            onAnnounceError={setLiveAnnouncement}
          />
        </fieldset>

        {/* Group 4: Relationship Tag Sample Floor */}
        <fieldset className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2.5 t-radius m-0 min-w-0">
          <legend className="px-1 t-font-display font-bold text-xs uppercase tracking-wider text-muted">
            RELATIONSHIP SAMPLE FLOOR
          </legend>
          <p className="text-xs text-muted leading-relaxed m-0">
            Prevents premature <span className="font-bold t-text-strong">NEMESIS</span> or{' '}
            <span className="font-bold t-text-strong">PRIME DUO</span> stamps after a single match by
            requiring a minimum head-to-head sample size (showing neutral{' '}
            <span className="t-font-mono font-bold">RECENT ALLY / RECENT FOE</span> below floor).
          </p>
          <ThresholdNumberField
            id="stamping-relationship-min-games"
            name="relationship_min_games"
            label="MIN H2H GAMES FOR VERDICT"
            hint="Encounters in role before verdict tag"
            value={stampingConfig.relationship_min_games}
            bounds={STAMPING_BOUNDS.relationship_min_games}
            onCommit={handleCommitNumber}
            onAnnounceError={setLiveAnnouncement}
          />
        </fieldset>
      </form>
    </section>
  );
};

import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { DateRangePreset, FilterArea, StatMode } from '../../context/DashboardContext';
import { normalizeActiveModeKey } from '../../utils/broadcast';

export type AreaFilterScopeId = FilterArea | 'dossier';

export interface AreaFilterBarProps {
  area: AreaFilterScopeId;
  ariaLabel: string;
  /** Concrete mode key ('11', '13', '10', '27'..'30', '34', 'casual'). */
  playlist?: string;
  onPlaylistChange?: (mode: string) => void;
  showModeFilter?: boolean;
  /** Optional timeframe controls (rendered only in areas where timeframe filtering applies). */
  dateRange?: DateRangePreset;
  onDateRangeChange?: (range: DateRangePreset) => void;
  customFrom?: string;
  onCustomFromChange?: (date: string) => void;
  customTo?: string;
  onCustomToChange?: (date: string) => void;
  showCustomCalendar?: boolean;
  onToggleCustomCalendar?: () => void;
  /** Optional AVG / G vs TOTALS toggle (rendered only in areas with aggregate stat tables). */
  statMode?: StatMode;
  onStatModeChange?: (mode: StatMode) => void;
  /** Optional area-specific leading / trailing slots (e.g., LIVE badge, COLUMNS, HIST toggle). */
  leadingSlot?: React.ReactNode;
  trailingSlot?: React.ReactNode;
  /** Optional reset action chip when non-default filters are active. */
  showReset?: boolean;
  onReset?: () => void;
  /** When true, omits outer container border/padding so it embeds cleanly inside a parent filter card. */
  embedded?: boolean;
  className?: string;
}

const EXTRA_MODE_ITEMS: ReadonlyArray<{ id: string; label: string; shortLabel: string }> = [
  { id: '27', label: 'HOOPS', shortLabel: 'HOOPS' },
  { id: '28', label: 'RUMBLE', shortLabel: 'RUMBLE' },
  { id: '29', label: 'DROPSHOT', shortLabel: 'DROPSHOT' },
  { id: '30', label: 'SNOW DAY', shortLabel: 'SNOW DAY' },
  { id: '34', label: 'TOURNAMENTS', shortLabel: 'TOURNAMENT' },
];

const EXTRA_MODE_SET = new Set(EXTRA_MODE_ITEMS.map((item) => item.id));

const TOOLBAR_TOGGLE_BTN =
  'toolbar-btn px-2.5 py-1 t-font-display font-bold text-xs uppercase tracking-wider whitespace-nowrap transition-colors duration-120 cursor-pointer surface-plaque t-text-muted hover:text-ink';

const TOOLBAR_ACTION_BTN =
  'toolbar-btn border border-outline px-2.5 py-1 t-font-display font-bold text-xs uppercase tracking-wider text-muted hover:text-ink hover:border-ink transition-colors duration-120 whitespace-nowrap t-radius cursor-pointer surface-plaque';

export const AreaFilterBar: React.FC<AreaFilterBarProps> = ({
  area,
  ariaLabel,
  playlist = '11',
  onPlaylistChange,
  showModeFilter = true,
  dateRange,
  onDateRangeChange,
  customFrom = '',
  onCustomFromChange,
  customTo = '',
  onCustomToChange,
  showCustomCalendar = false,
  onToggleCustomCalendar,
  statMode,
  onStatModeChange,
  leadingSlot,
  trailingSlot,
  showReset = false,
  onReset,
  embedded = false,
  className = '',
}) => {
  const [extraOpen, setExtraOpen] = useState(false);
  const [alignExtraRight, setAlignExtraRight] = useState(false);

  const toolbarRef = useRef<HTMLDivElement>(null);
  const extraWrapperRef = useRef<HTMLDivElement>(null);
  const extraTriggerRef = useRef<HTMLButtonElement>(null);
  const extraMenuRef = useRef<HTMLDivElement>(null);
  const openWithKeyboardRef = useRef(false);

  const normalizedMode = useMemo(() => normalizeActiveModeKey(playlist), [playlist]);
  const activeExtra = useMemo(
    () => EXTRA_MODE_ITEMS.find((item) => item.id === normalizedMode),
    [normalizedMode],
  );

  const closeExtraAndRestoreFocus = useCallback(() => {
    setExtraOpen(false);
    extraTriggerRef.current?.focus({ preventScroll: true });
  }, []);

  useEffect(() => {
    if (!extraOpen) return;
    const trigger = extraTriggerRef.current;
    if (trigger && typeof window !== 'undefined') {
      const rect = trigger.getBoundingClientRect();
      const menuWidth = 208;
      setAlignExtraRight(rect.left + menuWidth > window.innerWidth - 12 && rect.right >= menuWidth);
    }
    if (openWithKeyboardRef.current) {
      openWithKeyboardRef.current = false;
      const firstItem = extraMenuRef.current?.querySelector<HTMLButtonElement>('button');
      firstItem?.focus({ preventScroll: true });
    }
  }, [extraOpen]);

  useEffect(() => {
    if (!extraOpen) return;

    const onPointerDown = (e: PointerEvent | MouseEvent) => {
      if (extraWrapperRef.current && !extraWrapperRef.current.contains(e.target as Node)) {
        setExtraOpen(false);
      }
    };

    const onFocusIn = (e: FocusEvent) => {
      if (extraWrapperRef.current && !extraWrapperRef.current.contains(e.target as Node)) {
        setExtraOpen(false);
      }
    };

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        closeExtraAndRestoreFocus();
      }
    };

    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('focusin', onFocusIn);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('focusin', onFocusIn);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [extraOpen, closeExtraAndRestoreFocus]);

  const handleToolbarKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (typeof HTMLElement !== 'undefined' && 'focusGroup' in HTMLElement.prototype) {
      return;
    }
    if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(e.key)) return;
    const target = e.target as HTMLElement;
    if (target.tagName === 'SELECT' || target.tagName === 'INPUT') return;
    if (extraMenuRef.current?.contains(target)) return;

    const bar = toolbarRef.current;
    if (!bar) return;
    const controls = Array.from(
      bar.querySelectorAll<HTMLElement>('button:not([disabled]), select:not([disabled])'),
    ).filter((el) => !extraMenuRef.current?.contains(el));
    const idx = controls.indexOf(target);
    if (idx < 0 || controls.length === 0) return;

    const rtl = typeof getComputedStyle === 'function' && getComputedStyle(bar).direction === 'rtl';
    let nextIdx = idx;
    if (e.key === 'Home') nextIdx = 0;
    else if (e.key === 'End') nextIdx = controls.length - 1;
    else if (e.key === 'ArrowRight') nextIdx = (idx + (rtl ? -1 : 1) + controls.length) % controls.length;
    else if (e.key === 'ArrowLeft') nextIdx = (idx + (rtl ? 1 : -1) + controls.length) % controls.length;

    e.preventDefault();
    controls[nextIdx]?.focus({ preventScroll: true });
  };

  const handleExtraMenuKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      closeExtraAndRestoreFocus();
      return;
    }
    if (e.key === 'Tab') {
      setExtraOpen(false);
      return;
    }
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) return;
    const menu = extraMenuRef.current;
    if (!menu) return;
    const items = Array.from(menu.querySelectorAll<HTMLButtonElement>('button:not([disabled])'));
    if (items.length === 0) return;
    const current = items.indexOf(document.activeElement as HTMLButtonElement);
    const next =
      e.key === 'Home'
        ? 0
        : e.key === 'End'
        ? items.length - 1
        : current < 0
        ? 0
        : (current + (e.key === 'ArrowUp' ? -1 : 1) + items.length) % items.length;
    e.preventDefault();
    e.stopPropagation();
    items[next]?.focus({ preventScroll: true });
  };

  const selectMode = (mode: string) => {
    setExtraOpen(false);
    onPlaylistChange?.(mode);
  };

  const showTimeframe = dateRange !== undefined && Boolean(onDateRangeChange);
  const showStatModeToggle = statMode !== undefined && Boolean(onStatModeChange);
  const showCalendarRow = showTimeframe && (showCustomCalendar || dateRange === 'custom');
  const extraBtnId = `${area}-btn-mode-extra`;

  return (
    <div
      data-testid={`${area}-filter-bar`}
      data-filter-area={area}
      className={
        embedded
          ? `flex flex-col gap-2 font-display ${className}`
          : `surface-plaque border border-line t-radius font-display ${className}`
      }
    >
      <div
        ref={toolbarRef}
        role="toolbar"
        aria-label={ariaLabel}
        onKeyDown={handleToolbarKeyDown}
        {...({ focusgroup: 'toolbar wrap' } as Record<string, string>)}
        className={`${
          embedded ? '' : 'px-3.5 py-2'
        } flex flex-wrap items-center justify-between gap-x-3 gap-y-2 text-xs`}
      >
        {/* Left Cluster: Optional Leading Slot + Concrete Game Mode Switcher */}
        <div className="flex flex-wrap items-center gap-2 min-w-0">
          {leadingSlot}

          {showModeFilter && onPlaylistChange && (
            <div className="flex flex-wrap items-center gap-1.5 min-w-0">
              <span className="t-font-display font-bold text-[11px] uppercase tracking-widest t-text-muted hidden sm:inline">
                MODE:
              </span>
              <div
                role="group"
                aria-label="Game mode filter"
                className="inline-flex flex-wrap items-center t-border t-radius"
              >
                <button
                  type="button"
                  data-mode-btn="11"
                  aria-pressed={normalizedMode === '11'}
                  onClick={() => selectMode('11')}
                  className={`live-pl-btn prof-pl-btn ${TOOLBAR_TOGGLE_BTN}`}
                >
                  2V2
                </button>
                <button
                  type="button"
                  data-mode-btn="13"
                  aria-pressed={normalizedMode === '13'}
                  onClick={() => selectMode('13')}
                  className={`live-pl-btn prof-pl-btn ${TOOLBAR_TOGGLE_BTN} border-l t-border`}
                >
                  3V3
                </button>
                <button
                  type="button"
                  data-mode-btn="10"
                  aria-pressed={normalizedMode === '10'}
                  onClick={() => selectMode('10')}
                  className={`live-pl-btn prof-pl-btn ${TOOLBAR_TOGGLE_BTN} border-l t-border`}
                >
                  1V1
                </button>

                {/* EXTRA ▾ Viewport-Safe Mode Menu */}
                <div ref={extraWrapperRef} className="relative border-l t-border">
                  <button
                    ref={extraTriggerRef}
                    type="button"
                    id={extraBtnId}
                    data-mode-btn="extra"
                    aria-haspopup="menu"
                    aria-expanded={extraOpen}
                    onClick={(e) => {
                      openWithKeyboardRef.current = e.detail === 0;
                      setExtraOpen((prev) => !prev);
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'ArrowDown') {
                        e.preventDefault();
                        e.stopPropagation();
                        openWithKeyboardRef.current = true;
                        setExtraOpen(true);
                      }
                    }}
                    className={`toolbar-btn px-2.5 py-1 t-font-display font-bold text-xs uppercase tracking-wider whitespace-nowrap transition-colors duration-120 cursor-pointer flex items-center gap-1 ${
                      EXTRA_MODE_SET.has(normalizedMode)
                        ? 't-bg-blue-ink t-text-on-accent'
                        : extraOpen
                        ? 't-bg-panel-hover t-text-strong'
                        : 'surface-plaque t-text-muted hover:text-ink'
                    }`}
                  >
                    <span>{activeExtra ? `EXTRA · ${activeExtra.shortLabel}` : 'EXTRA'}</span>
                    <span aria-hidden="true">▾</span>
                  </button>

                  {extraOpen && (
                    <div
                      ref={extraMenuRef}
                      role="menu"
                      aria-labelledby={extraBtnId}
                      onKeyDown={handleExtraMenuKeyDown}
                      {...({ focusgroup: 'menu nomemory' } as Record<string, string>)}
                      className={`extra-mode-dropdown absolute top-full mt-1 z-50 min-w-[196px] t-bg-modal bg-panel border border-outline shadow-lg py-1 flex flex-col t-radius ${
                        alignExtraRight ? 'right-0' : 'left-0'
                      }`}
                    >
                      {EXTRA_MODE_ITEMS.map((item) => {
                        const checked = normalizedMode === item.id;
                        return (
                          <button
                            key={item.id}
                            type="button"
                            role="menuitemradio"
                            aria-checked={checked}
                            onClick={() => {
                              onPlaylistChange(item.id);
                              closeExtraAndRestoreFocus();
                            }}
                            className={`toolbar-btn w-full px-3 py-1.5 text-left t-font-display font-bold text-xs uppercase tracking-wider flex items-center justify-between gap-3 cursor-pointer ${
                              checked
                                ? 't-bg-blue-ink t-text-on-accent'
                                : 't-text-ink hover:bg-panel-hover'
                            }`}
                          >
                            <span>{item.label}</span>
                            {checked && <span aria-hidden="true">✓</span>}
                          </button>
                        );
                      })}
                    </div>
                  )}
                </div>

                <button
                  type="button"
                  data-mode-btn="casual"
                  aria-pressed={normalizedMode === 'casual'}
                  onClick={() => selectMode('casual')}
                  className={`live-pl-btn prof-pl-btn ${TOOLBAR_TOGGLE_BTN} border-l t-border`}
                >
                  CASUAL
                </button>
              </div>
            </div>
          )}
        </div>

        {/* Right Cluster: Contextual Timeframe, Stat Aggregation, Trailing Slot & Reset */}
        <div className="flex flex-wrap items-center gap-2 shrink-0">
          {showTimeframe && (
            <div
              role="group"
              aria-label="Timeframe filter"
              className="flex items-center gap-1.5 t-bg-page t-border px-2 py-0.5 t-radius whitespace-nowrap"
            >
              <span className="t-font-display font-bold text-[11px] uppercase tracking-wider t-text-muted hidden xl:inline">
                TIMEFRAME:
              </span>
              <select
                id={`${area}-date-range`}
                data-testid={`${area}-date-range`}
                aria-label="Timeframe"
                value={dateRange}
                onChange={(e) => onDateRangeChange?.(e.target.value as DateRangePreset)}
                className="t-bg-page t-text-strong text-xs t-font-display font-bold uppercase tracking-wider px-1.5 py-0.5 t-border focus:outline-none cursor-pointer"
              >
                <option value="all">ALL TIME</option>
                <option value="today">TODAY / SESSION</option>
                <option value="7d">LAST 7 DAYS</option>
                <option value="30d">LAST 30 DAYS</option>
                <option value="custom">CUSTOM RANGE...</option>
              </select>
              {onToggleCustomCalendar && (
                <button
                  type="button"
                  data-testid={`${area}-cal-toggle`}
                  aria-pressed={showCustomCalendar || dateRange === 'custom'}
                  onClick={onToggleCustomCalendar}
                  className="toolbar-btn px-1.5 py-0.5 t-bg-page t-border t-font-display font-bold text-xs uppercase tracking-wider t-text-strong hover:opacity-80 cursor-pointer whitespace-nowrap"
                  title="Pick custom start & end dates"
                >
                  ◆ DATES
                </button>
              )}
            </div>
          )}

          {showStatModeToggle && (
            <div
              className="inline-flex t-border t-radius overflow-hidden"
              role="group"
              aria-label="Stat aggregation mode"
            >
              <button
                type="button"
                data-stat-mode="avg"
                aria-pressed={statMode === 'avg'}
                onClick={() => onStatModeChange?.('avg')}
                className={TOOLBAR_TOGGLE_BTN}
              >
                AVG / G
              </button>
              <button
                type="button"
                data-stat-mode="total"
                aria-pressed={statMode === 'total'}
                onClick={() => onStatModeChange?.('total')}
                className={`${TOOLBAR_TOGGLE_BTN} border-l t-border`}
              >
                TOTALS
              </button>
            </div>
          )}

          {trailingSlot}

          {showReset && onReset && (
            <button
              type="button"
              data-testid={`${area}-reset-filters`}
              onClick={onReset}
              className={TOOLBAR_ACTION_BTN}
              title="Reset filters for this view"
            >
              RESET
            </button>
          )}
        </div>
      </div>

      {/* Area-Local On-Demand Custom Date Range Picker Strip */}
      {showCalendarRow && onCustomFromChange && onCustomToChange && (
        <div
          data-testid="custom-calendar-bar"
          className={`${embedded ? 'border-t pt-2' : 'border-t px-3.5 py-1.5'} border-line flex flex-wrap items-center gap-2 text-xs`}
        >
          <span className="t-font-display font-bold uppercase tracking-wider t-text-muted">
            CUSTOM DATE FILTER:
          </span>
          <label
            htmlFor={`${area}-custom-date-from`}
            className="text-xs t-font-display font-bold t-text-muted uppercase"
          >
            FROM
          </label>
          <input
            id={`${area}-custom-date-from`}
            data-testid="custom-date-from"
            type="date"
            value={customFrom}
            onChange={(e) => onCustomFromChange(e.target.value)}
            className="t-bg-page t-text-strong t-border px-2 py-0.5 text-xs t-font-mono t-radius focus:outline-none cursor-pointer"
          />
          <span className="t-text-muted font-bold">→</span>
          <label
            htmlFor={`${area}-custom-date-to`}
            className="text-xs t-font-display font-bold t-text-muted uppercase"
          >
            TO
          </label>
          <input
            id={`${area}-custom-date-to`}
            data-testid="custom-date-to"
            type="date"
            value={customTo}
            onChange={(e) => onCustomToChange(e.target.value)}
            className="t-bg-page t-text-strong t-border px-2 py-0.5 text-xs t-font-mono t-radius focus:outline-none cursor-pointer"
          />
        </div>
      )}
    </div>
  );
};

import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { DateRangePreset, useDashboard } from '../../context/DashboardContext';
import { playlistFilterToParam } from '../../utils/broadcast';
import type { DashboardTab } from '../../types/api';

interface ContextFilterBarProps {
  activeTab: DashboardTab;
  inMatch: boolean;
  onOpenColumns?: () => void;
}

const EXTRA_MODE_ITEMS: ReadonlyArray<{ id: string; label: string; shortLabel: string }> = [
  { id: '27', label: 'HOOPS', shortLabel: 'HOOPS' },
  { id: '28', label: 'RUMBLE', shortLabel: 'RUMBLE' },
  { id: '29', label: 'DROPSHOT', shortLabel: 'DROPSHOT' },
  { id: '30', label: 'SNOW DAY', shortLabel: 'SNOW DAY' },
  { id: '34', label: 'TOURNAMENTS', shortLabel: 'TOURNAMENT' },
  { id: 'extra', label: 'ALL EXTRA MODES', shortLabel: 'ALL EXTRA' },
];

const EXTRA_MODE_SET = new Set(EXTRA_MODE_ITEMS.map((item) => item.id));

function normalizeModeKey(raw: string): string {
  const param = playlistFilterToParam(raw);
  return param || 'all';
}

const TOOLBAR_TOGGLE_BTN =
  'toolbar-btn px-2.5 py-1 t-font-display font-bold text-xs uppercase tracking-wider whitespace-nowrap transition-colors duration-120 cursor-pointer surface-plaque t-text-muted hover:text-ink';

const TOOLBAR_ACTION_BTN =
  'toolbar-btn border border-outline px-2.5 py-1 t-font-display font-bold text-xs uppercase tracking-wider text-muted hover:text-ink hover:border-ink transition-colors duration-120 whitespace-nowrap t-radius cursor-pointer surface-plaque';

export const ContextFilterBar: React.FC<ContextFilterBarProps> = ({
  activeTab,
  inMatch,
  onOpenColumns,
}) => {
  const {
    statMode,
    setStatMode,
    activePlaylist,
    setActivePlaylist,
    dateRange,
    setDateRange,
    showCustomCalendar,
    setShowCustomCalendar,
    showLiveHistData,
    setShowLiveHistData,
  } = useDashboard();

  const [extraOpen, setExtraOpen] = useState(false);
  const [alignExtraRight, setAlignExtraRight] = useState(false);

  const toolbarRef = useRef<HTMLDivElement>(null);
  const extraWrapperRef = useRef<HTMLDivElement>(null);
  const extraTriggerRef = useRef<HTMLButtonElement>(null);
  const extraMenuRef = useRef<HTMLDivElement>(null);
  const openWithKeyboardRef = useRef(false);

  const normalizedMode = useMemo(() => normalizeModeKey(activePlaylist), [activePlaylist]);
  const activeExtra = useMemo(
    () => EXTRA_MODE_ITEMS.find((item) => item.id === normalizedMode),
    [normalizedMode],
  );

  const closeExtraAndRestoreFocus = useCallback((fromPointer = false) => {
    setExtraOpen(false);
    const trigger = extraTriggerRef.current;
    if (trigger) {
      if (fromPointer) {
        trigger.focus({ preventScroll: true });
      } else {
        trigger.focus({ preventScroll: true });
      }
    }
  }, []);

  // Measure trigger viewport space when EXTRA menu opens so it never clips at 1280px
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

  // Light-dismiss on outside pointerdown or focus leaving the menu wrapper
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
        closeExtraAndRestoreFocus(false);
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

  // Roving arrow-key navigation fallback for horizontal toolbar when native focusgroup is absent
  const handleToolbarKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (
      typeof HTMLElement !== 'undefined' &&
      'focusGroup' in HTMLElement.prototype
    ) {
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
      closeExtraAndRestoreFocus(false);
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
    setActivePlaylist(mode);
  };

  const defaultModeForTab = activeTab === 'history' ? 'all' : '11';
  const showReset =
    dateRange !== 'all' ||
    showCustomCalendar ||
    (activeTab === 'history'
      ? normalizedMode !== 'all'
      : normalizedMode !== '11' && normalizedMode !== 'all');

  const handleResetScope = () => {
    setDateRange('all');
    setShowCustomCalendar(false);
    setActivePlaylist(defaultModeForTab);
  };

  return (
    <div
      ref={toolbarRef}
      id="context-filter-bar"
      data-testid="context-filter-bar"
      role="toolbar"
      aria-label="Analytics context filters"
      onKeyDown={handleToolbarKeyDown}
      {...({ focusgroup: 'toolbar wrap' } as Record<string, string>)}
      className="border-t border-line surface-plaque"
    >
      <div className="max-w-[1680px] mx-auto px-4 sm:px-6 py-1.5 flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5 text-xs">
        {/* Left Cluster: Unified Game Mode Switcher */}
        <div className="flex flex-wrap items-center gap-1.5 min-w-0">
          <span className="t-font-display font-bold text-[11px] uppercase tracking-widest t-text-muted hidden sm:inline mr-0.5">
            MODE:
          </span>
          <div
            role="group"
            aria-label="Game mode filter"
            className="inline-flex flex-wrap items-center t-border t-radius"
          >
            <button
              type="button"
              id="btn-mode-2v2"
              aria-pressed={normalizedMode === '11'}
              onClick={() => selectMode('11')}
              className={TOOLBAR_TOGGLE_BTN}
            >
              2V2
            </button>
            <button
              type="button"
              id="btn-mode-3v3"
              aria-pressed={normalizedMode === '13'}
              onClick={() => selectMode('13')}
              className={`${TOOLBAR_TOGGLE_BTN} border-l t-border`}
            >
              3V3
            </button>
            <button
              type="button"
              id="btn-mode-1v1"
              aria-pressed={normalizedMode === '10'}
              onClick={() => selectMode('10')}
              className={`${TOOLBAR_TOGGLE_BTN} border-l t-border`}
            >
              1V1
            </button>

            {/* EXTRA ▾ Viewport-Safe Mode Menu */}
            <div ref={extraWrapperRef} className="relative border-l t-border">
              <button
                ref={extraTriggerRef}
                type="button"
                id="btn-mode-extra"
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
                  id="extra-modes-menu"
                  role="menu"
                  aria-labelledby="btn-mode-extra"
                  onKeyDown={handleExtraMenuKeyDown}
                  {...({ focusgroup: 'menu nomemory' } as Record<string, string>)}
                  className={`extra-mode-dropdown absolute top-full mt-1 z-50 min-w-[196px] t-bg-modal bg-panel border border-outline shadow-lg py-1 flex flex-col t-radius ${
                    alignExtraRight ? 'right-0' : 'left-0'
                  }`}
                >
                  {EXTRA_MODE_ITEMS.map((item, idx) => {
                    const checked = normalizedMode === item.id;
                    return (
                      <React.Fragment key={item.id}>
                        {idx === EXTRA_MODE_ITEMS.length - 1 && (
                          <hr className="border-t border-line my-1" />
                        )}
                        <button
                          type="button"
                          role="menuitemradio"
                          aria-checked={checked}
                          onClick={(e) => {
                            const fromPointer = e.detail !== 0;
                            setActivePlaylist(item.id);
                            closeExtraAndRestoreFocus(fromPointer);
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
                      </React.Fragment>
                    );
                  })}
                </div>
              )}
            </div>

            <button
              type="button"
              id="btn-mode-casual"
              aria-pressed={normalizedMode === 'casual'}
              onClick={() => selectMode('casual')}
              className={`${TOOLBAR_TOGGLE_BTN} border-l t-border`}
            >
              CASUAL
            </button>
            <button
              type="button"
              id="btn-mode-all"
              aria-pressed={normalizedMode === 'all'}
              onClick={() => selectMode('all')}
              className={`${TOOLBAR_TOGGLE_BTN} border-l t-border`}
            >
              {activeTab === 'history' ? 'ALL MATCHES' : 'ALL'}
            </button>
          </div>
        </div>

        {/* Right Cluster: Timeframe, Contextual Stat Mode, Live Match Controls & Reset */}
        <div className="flex flex-wrap items-center gap-2 shrink-0">
          {/* Timeframe Selector */}
          <div
            role="group"
            aria-label="Timeframe filter"
            className="flex items-center gap-1.5 t-bg-page t-border px-2 py-0.5 t-radius whitespace-nowrap"
          >
            {activeTab === 'session' ? (
              <span className="t-font-display font-bold text-[11px] uppercase tracking-wider t-text-blue hidden xl:inline">
                SESSION SCOPE:
              </span>
            ) : (
              <span className="t-font-display font-bold text-[11px] uppercase tracking-wider t-text-muted hidden xl:inline">
                TIMEFRAME:
              </span>
            )}
            <select
              id="global-date-range"
              aria-label="Timeframe"
              value={dateRange}
              onChange={(e) => setDateRange(e.target.value as DateRangePreset)}
              className="t-bg-page t-text-strong text-xs t-font-display font-bold uppercase tracking-wider px-1.5 py-0.5 t-border focus:outline-none cursor-pointer"
            >
              <option value="all">ALL TIME</option>
              <option value="today">TODAY / SESSION</option>
              <option value="7d">LAST 7 DAYS</option>
              <option value="30d">LAST 30 DAYS</option>
              <option value="custom">CUSTOM RANGE...</option>
            </select>
            <button
              type="button"
              id="btn-cal-toggle"
              aria-pressed={showCustomCalendar || dateRange === 'custom'}
              onClick={() => {
                if (!showCustomCalendar && dateRange !== 'custom') {
                  setDateRange('custom');
                } else {
                  setShowCustomCalendar((prev) => !prev);
                }
              }}
              className="toolbar-btn px-1.5 py-0.5 t-bg-page t-border t-font-display font-bold text-xs uppercase tracking-wider t-text-strong hover:opacity-80 cursor-pointer whitespace-nowrap"
              title="Pick custom start & end dates"
            >
              ◆ DATES
            </button>
          </div>

          {/* Contextual AVG / G vs TOTALS Toggle (Hidden on HISTORY where no aggregate stat table exists) */}
          {activeTab !== 'history' && (
            <>
              <span
                role="separator"
                aria-orientation="vertical"
                className="toolbar-separator hidden sm:inline-block h-4 border-l border-line"
              />
              <div
                className="inline-flex t-border t-radius overflow-hidden"
                role="group"
                aria-label="Stat aggregation mode"
              >
                <button
                  type="button"
                  id="btn-stat-avg"
                  aria-pressed={statMode === 'avg'}
                  onClick={() => setStatMode('avg')}
                  className={TOOLBAR_TOGGLE_BTN}
                >
                  AVG / G
                </button>
                <button
                  type="button"
                  id="btn-stat-total"
                  aria-pressed={statMode === 'total'}
                  onClick={() => setStatMode('total')}
                  className={`${TOOLBAR_TOGGLE_BTN} border-l t-border`}
                >
                  TOTALS
                </button>
              </div>
            </>
          )}

          {/* Contextual Live In-Match Controls */}
          {activeTab === 'live' && inMatch && (
            <>
              <span
                role="separator"
                aria-orientation="vertical"
                className="toolbar-separator hidden sm:inline-block h-4 border-l border-line"
              />
              <div
                role="group"
                aria-label="Live match controls"
                className="flex flex-wrap items-center gap-1.5"
              >
                {onOpenColumns && (
                  <button type="button" onClick={onOpenColumns} className={TOOLBAR_ACTION_BTN}>
                    COLUMNS
                  </button>
                )}
                <button
                  type="button"
                  id="btn-toggle-live-hist"
                  aria-pressed={showLiveHistData}
                  onClick={() => setShowLiveHistData((prev) => !prev)}
                  className="toolbar-btn px-2.5 py-1 t-border t-radius t-font-display font-bold text-xs uppercase tracking-wider transition-colors cursor-pointer surface-plaque t-text-muted hover:text-ink whitespace-nowrap"
                  title="Show or hide historical WITH YOU / AGAINST YOU split tables on live player cards"
                >
                  {showLiveHistData ? 'HISTORICAL DATA: SHOWN' : 'HISTORICAL DATA: HIDDEN'}
                </button>
              </div>
            </>
          )}

          {/* Reset Scope Action Chip */}
          {showReset && (
            <button
              type="button"
              id="btn-reset-scope"
              data-testid="reset-scope-btn"
              onClick={handleResetScope}
              className={TOOLBAR_ACTION_BTN}
              title="Reset mode and timeframe filters"
            >
              RESET
            </button>
          )}
        </div>
      </div>
    </div>
  );
};

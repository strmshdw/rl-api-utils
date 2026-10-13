// Shared "Broadcast" UI primitives: section labels, page headers, segmented
// controls, ghost/primary buttons, W/L cell strips and small chips.
import React from 'react';
import { CELL_BG, OUTCOME_SLAB, Outcome, recordCells } from '../../utils/broadcast';

export const COLUMN_HEADER = 'font-display font-semibold text-[12px] tracking-t14 text-faint uppercase';
export const META_TEXT = 'font-display font-semibold text-[14px] tracking-t8 text-muted uppercase';
export const PANEL_LABEL = 'font-display font-semibold text-[13px] tracking-t14 text-muted-2 uppercase';

export const SectionLabel: React.FC<{ children: React.ReactNode; className?: string }> = ({ children, className = '' }) => (
  <div className={`font-display font-bold text-[14px] tracking-t16 text-muted uppercase ${className}`}>{children}</div>
);

interface PageHeaderProps {
  kicker: React.ReactNode;
  title: React.ReactNode;
  right?: React.ReactNode;
}

export const PageHeader: React.FC<PageHeaderProps> = ({ kicker, title, right }) => (
  <div className="flex flex-wrap justify-between items-end gap-x-6 gap-y-4 pt-10 pb-6">
    <div className="flex flex-col gap-1.5 min-w-0">
      <SectionLabel>{kicker}</SectionLabel>
      <h1 className="font-display italic font-extrabold text-[64px] leading-tightest uppercase">{title}</h1>
    </div>
    {right}
  </div>
);

/** Muted single-line message used for loading, empty and error states. */
export const EmptyLine: React.FC<{ children: React.ReactNode; tone?: 'muted' | 'loss' }> = ({ children, tone = 'muted' }) => (
  <p className={`font-display font-semibold text-[15px] tracking-t8 uppercase py-6 ${tone === 'loss' ? 'text-loss' : 'text-muted'}`}>
    {children}
  </p>
);

interface SegmentedProps<T extends string> {
  options: ReadonlyArray<readonly [T, string]>;
  value: T;
  onChange: (value: T) => void;
  label: string;
}

export function Segmented<T extends string>({ options, value, onChange, label }: SegmentedProps<T>) {
  return (
    <div role="group" aria-label={label} className="flex border border-outline shrink-0">
      {options.map(([id, text]) => {
        const active = id === value;
        return (
          <button
            key={id}
            type="button"
            aria-pressed={active}
            onClick={() => onChange(id)}
            className={`font-display font-bold text-[14px] tracking-t10 px-4 py-[7px] whitespace-nowrap transition-colors duration-120 ${
              active ? 'bg-ink text-page' : 'bg-transparent text-muted hover:text-ink'
            }`}
          >
            {text}
          </button>
        );
      })}
    </div>
  );
}

const GHOST = 'font-display font-bold text-[14px] tracking-t10 border px-3.5 py-2 whitespace-nowrap transition-colors duration-120 disabled:opacity-40 disabled:cursor-not-allowed';

interface GhostButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  tone?: 'muted' | 'ink' | 'danger';
}

export const GhostButton: React.FC<GhostButtonProps> = ({ tone = 'muted', className = '', type = 'button', ...rest }) => {
  const toneClass =
    tone === 'danger'
      ? 'border-outline text-muted enabled:hover:text-loss enabled:hover:border-loss'
      : tone === 'ink'
      ? 'border-outline text-ink enabled:hover:text-ink-strong enabled:hover:border-ink'
      : 'border-outline text-muted enabled:hover:text-ink enabled:hover:border-ink';
  return <button type={type} className={`${GHOST} ${toneClass} ${className}`} {...rest} />;
};

export const PrimaryButton: React.FC<React.ButtonHTMLAttributes<HTMLButtonElement>> = ({ className = '', type = 'button', ...rest }) => (
  <button
    type={type}
    className={`font-display font-extrabold text-[15px] tracking-t10 bg-ink text-page px-[18px] py-2.5 whitespace-nowrap transition-colors duration-120 enabled:hover:bg-blue-ink disabled:opacity-60 disabled:cursor-not-allowed ${className}`}
    {...rest}
  />
);

export const SearchInput: React.FC<React.InputHTMLAttributes<HTMLInputElement>> = ({ className = '', ...rest }) => (
  <input
    type="search"
    className={`flex-1 min-w-[200px] bg-panel border border-line text-ink font-display font-semibold text-[15px] tracking-t8 uppercase px-4 py-2.5 outline-none focus:border-outline ${className}`}
    {...rest}
  />
);

export const StatusDot: React.FC<{ className: string; size?: number }> = ({ className, size = 7 }) => (
  <span aria-hidden="true" className={`inline-block rounded-full shrink-0 ${className}`} style={{ width: size, height: size }} />
);

interface RecordCellsProps {
  wins: number;
  losses: number;
  count?: number;
  /** Size classes for each cell, e.g. "w-[10px] h-[18px]" or "flex-1 h-[14px]". */
  cellClass?: string;
  className?: string;
}

export const RecordCells: React.FC<RecordCellsProps> = ({ wins, losses, count = 5, cellClass = 'w-[10px] h-[18px]', className = '' }) => (
  <div className={`flex gap-[3px] ${className}`} aria-hidden="true">
    {recordCells(wins, losses, count).map((kind, i) => (
      <span key={i} data-cell={kind} className={`${cellClass} ${CELL_BG[kind]}`} />
    ))}
  </div>
);

export const MvpChip: React.FC<{ large?: boolean }> = ({ large = false }) => (
  <span
    className={`font-display font-bold tracking-t14 bg-ink text-page shrink-0 leading-normal ${
      large ? 'text-[12px] px-1.5 py-px' : 'text-[11px] px-[5px] py-px'
    }`}
  >
    MVP
  </span>
);

/** Full-height W/L slab at the start of a match row. */
export const ResultSlab: React.FC<{ outcome: Outcome; className?: string }> = ({ outcome, className = '' }) => (
  <div
    className={`h-full flex items-center justify-center font-display font-extrabold tracking-t6 ${
      outcome ? `${OUTCOME_SLAB[outcome]} text-page` : 'bg-line-2 text-muted'
    } ${className}`}
  >
    {outcome ?? '—'}
  </div>
);

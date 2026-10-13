import React from 'react';
import { ColumnConfig, StatColumnKey, PresetKey, STAT_COLUMNS, PRESETS } from '../../types/columns';
import { Modal } from '../common/Modal';
import { PrimaryButton, SectionLabel } from '../common/ui';

interface ColumnConfigModalProps {
  isOpen: boolean;
  onClose: () => void;
  config: ColumnConfig;
  toggleColumn: (key: StatColumnKey) => void;
  applyPreset: (preset: PresetKey) => void;
  resetToDefault: () => void;
  activePreset: PresetKey | 'custom';
}

export const ColumnConfigModal: React.FC<ColumnConfigModalProps> = ({
  isOpen,
  onClose,
  config,
  toggleColumn,
  applyPreset,
  resetToDefault,
  activePreset,
}) => (
  <Modal isOpen={isOpen} onClose={onClose} kicker="LIVE · PLAYER CARDS" title="COLUMNS" maxWidth={720}>
    <div className="px-8 pt-6 pb-2 flex flex-col gap-3">
      <SectionLabel>PRESETS</SectionLabel>
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
        {(Object.keys(PRESETS) as PresetKey[]).map((key) => {
          const preset = PRESETS[key];
          const active = activePreset === key;
          return (
            <button
              key={key}
              type="button"
              aria-pressed={active}
              onClick={() => applyPreset(key)}
              className={`text-left bg-panel hover:bg-panel-hover border-t-[3px] px-4 py-3.5 flex flex-col gap-1 transition-colors duration-120 ${
                active ? 'border-ink' : 'border-transparent'
              }`}
            >
              <span className={`font-bold text-[18px] tracking-t4 uppercase ${active ? 'text-ink-strong' : 'text-ink-2'}`}>{preset.name}</span>
              <span className="font-medium text-[13px] tracking-t4 text-muted">{preset.description}</span>
            </button>
          );
        })}
      </div>
    </div>

    <div className="px-8 pt-6 pb-7 flex flex-col gap-3">
      <div className="flex items-center justify-between gap-4">
        <SectionLabel>VISIBLE COLUMNS</SectionLabel>
        <button
          type="button"
          onClick={resetToDefault}
          className="font-semibold text-[13px] tracking-t12 text-muted hover:text-ink transition-colors duration-120"
        >
          RESET DEFAULTS
        </button>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-1.5">
        {STAT_COLUMNS.map((col) => {
          const checked = !!config[col.key];
          return (
            <label
              key={col.key}
              className="flex items-center gap-3.5 bg-panel hover:bg-panel-hover px-4 py-3 cursor-pointer select-none transition-colors duration-120"
            >
              <input type="checkbox" checked={checked} onChange={() => toggleColumn(col.key)} className="peer sr-only" />
              <span
                aria-hidden="true"
                className={`w-3.5 h-3.5 shrink-0 border peer-focus-visible:outline peer-focus-visible:outline-1 peer-focus-visible:outline-blue-ink ${
                  checked ? 'bg-ink border-ink' : 'border-outline'
                }`}
              />
              <span className="flex flex-col min-w-0 flex-1">
                <span className={`font-bold text-[17px] tracking-t4 uppercase ${checked ? 'text-ink' : 'text-muted'}`}>{col.label}</span>
                <span className="font-medium text-[13px] tracking-t4 text-muted-2 truncate">{col.description}</span>
              </span>
              <span className="font-semibold text-[12px] tracking-t14 text-faint">{col.shortLabel}</span>
            </label>
          );
        })}
      </div>
    </div>

    <div className="flex items-center justify-between gap-4 px-8 py-3.5 border-t border-line">
      <span className="font-semibold text-[13px] tracking-t12 text-muted-2">SELECTIONS PERSIST ACROSS RELOADS</span>
      <PrimaryButton onClick={onClose}>DONE</PrimaryButton>
    </div>
  </Modal>
);

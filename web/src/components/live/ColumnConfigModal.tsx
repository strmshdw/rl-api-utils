import React, { useEffect, useRef } from 'react';
import { ColumnConfig, StatColumnKey, PresetKey, STAT_COLUMNS, PRESETS } from '../../types/columns';
import { X, RotateCcw, Check, Eye } from 'lucide-react';

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
}) => {
  const dialogRef = useRef<HTMLDialogElement | null>(null);

  // Synchronize modal state with native dialog
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;

    if (isOpen) {
      if (!dialog.open) {
        dialog.showModal();
      }
    } else {
      if (dialog.open) {
        dialog.close();
      }
    }
  }, [isOpen]);

  // Handle ESC and Light-Dismiss fallback for unsupported browsers
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;

    const handleCancel = (e: Event) => {
      e.preventDefault();
      onClose();
    };

    // Fallback light-dismiss if closedby is not supported
    const handleClick = (e: MouseEvent) => {
      if (e.target === dialog) {
        const rect = dialog.getBoundingClientRect();
        const isInDialog =
          rect.top <= e.clientY &&
          e.clientY <= rect.top + rect.height &&
          rect.left <= e.clientX &&
          e.clientX <= rect.left + rect.width;
        if (!isInDialog) {
          onClose();
        }
      }
    };

    dialog.addEventListener('cancel', handleCancel);
    dialog.addEventListener('click', handleClick);

    return () => {
      dialog.removeEventListener('cancel', handleCancel);
      dialog.removeEventListener('click', handleClick);
    };
  }, [onClose]);

  if (!isOpen) return null;

  return (
    <dialog
      ref={dialogRef}
      closedby="any"
      aria-labelledby="column-customizer-title"
      className="fixed inset-0 m-auto p-0 bg-transparent backdrop:bg-slate-950/80 backdrop:backdrop-blur-sm z-50 max-w-xl w-[90vw] rounded-2xl border border-slate-800 shadow-2xl overflow-hidden text-slate-100"
    >
      <div className="bg-slate-900 flex flex-col max-h-[85vh]">
        {/* Modal Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-slate-800 bg-slate-950/50">
          <div>
            <h2 id="column-customizer-title" className="text-lg font-bold text-slate-100 flex items-center gap-2">
              <Eye className="w-5 h-5 text-cyan-400" />
              Configure Stat Columns
            </h2>
            <p className="text-xs text-slate-400 mt-0.5">
              Customize which performance and skill metrics appear in live match tables.
            </p>
          </div>
          <button
            onClick={onClose}
            aria-label="Close dialog"
            className="p-1.5 rounded-lg text-slate-400 hover:text-slate-100 hover:bg-slate-800 transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Modal Body */}
        <div className="p-6 overflow-y-auto space-y-6">
          {/* Quick Presets Selection */}
          <div>
            <span className="block text-xs font-bold uppercase tracking-wider text-slate-400 mb-2.5">
              Presets
            </span>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-2.5">
              {(Object.keys(PRESETS) as PresetKey[]).map(key => {
                const preset = PRESETS[key];
                const isActive = activePreset === key;
                return (
                  <button
                    key={key}
                    type="button"
                    onClick={() => applyPreset(key)}
                    className={`flex flex-col text-left p-3 rounded-xl border transition-all ${
                      isActive
                        ? 'border-cyan-500 bg-cyan-500/10 text-cyan-300 shadow-[0_0_12px_rgba(6,182,212,0.2)]'
                        : 'border-slate-800 bg-slate-800/40 text-slate-300 hover:border-slate-700 hover:bg-slate-800'
                    }`}
                  >
                    <div className="flex items-center justify-between w-full">
                      <span className="text-xs font-bold">{preset.name}</span>
                      {isActive && <Check className="w-4 h-4 text-cyan-400" />}
                    </div>
                    <span className="text-[11px] text-slate-400 mt-1 line-clamp-2">
                      {preset.description}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>

          {/* Interactive Column Toggles */}
          <div>
            <div className="flex items-center justify-between mb-2.5">
              <span className="text-xs font-bold uppercase tracking-wider text-slate-400">
                Visible Columns (10 Available)
              </span>
              <button
                type="button"
                onClick={resetToDefault}
                className="text-xs text-slate-400 hover:text-cyan-400 flex items-center gap-1 transition-colors"
              >
                <RotateCcw className="w-3.5 h-3.5" />
                Reset Defaults
              </button>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              {STAT_COLUMNS.map(col => {
                const checked = !!config[col.key];
                return (
                  <label
                    key={col.key}
                    className={`flex items-start gap-3 p-3 rounded-xl border cursor-pointer select-none transition-all ${
                      checked
                        ? 'bg-slate-800/60 border-slate-700 text-slate-100'
                        : 'bg-slate-950/40 border-slate-800 text-slate-500 hover:border-slate-750'
                    }`}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => toggleColumn(col.key)}
                      className="mt-1 w-4 h-4 rounded border-slate-700 bg-slate-800 text-cyan-500 focus:ring-cyan-500 focus:ring-offset-slate-900 transition-colors"
                    />
                    <div className="flex-1">
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-bold">{col.label}</span>
                        <span className="text-[10px] font-mono text-slate-400 bg-slate-800 px-1 rounded">
                          {col.shortLabel}
                        </span>
                      </div>
                      <p className="text-[11px] text-slate-400 mt-0.5">{col.description}</p>
                    </div>
                  </label>
                );
              })}
            </div>
          </div>
        </div>

        {/* Modal Footer */}
        <div className="flex items-center justify-between px-6 py-4 border-t border-slate-800 bg-slate-950/50">
          <span className="text-xs text-slate-400">
            Selections persist automatically across reloads.
          </span>
          <button
            type="button"
            onClick={onClose}
            className="px-5 py-2 rounded-xl text-xs font-bold text-slate-950 bg-cyan-400 hover:bg-cyan-300 transition-colors shadow-[0_0_12px_rgba(6,182,212,0.3)]"
          >
            Done
          </button>
        </div>
      </div>
    </dialog>
  );
};

import React, { useEffect, useId, useRef } from 'react';
import { GhostButton, SectionLabel } from './ui';

interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  /** Small caps label above the title, e.g. "PLAYER PROFILE · STEAM". */
  kicker?: React.ReactNode;
  /** Italic display title. */
  title: React.ReactNode;
  /** Optional line under the title (IDs, links, timestamps). */
  meta?: React.ReactNode;
  children: React.ReactNode;
  /** Panel max width in px (default 980). */
  maxWidth?: number;
}

/**
 * Broadcast dialog built on the native <dialog>: full-viewport scrim, panel
 * aligned to the top with a 4px ink rule. Closes on Esc, the ✕ button, or a
 * click on the backdrop.
 */
export const Modal: React.FC<ModalProps> = ({ isOpen, onClose, kicker, title, meta, children, maxWidth = 980 }) => {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleId = useId();

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog || !isOpen) return;
    if (!dialog.open) {
      if (typeof dialog.showModal === 'function') dialog.showModal();
      else dialog.setAttribute('open', '');
    }
    return () => {
      if (dialog.open && typeof dialog.close === 'function') dialog.close();
    };
  }, [isOpen]);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const handleCancel = (e: Event) => {
      e.preventDefault();
      onClose();
    };
    dialog.addEventListener('cancel', handleCancel);
    return () => dialog.removeEventListener('cancel', handleCancel);
  }, [onClose, isOpen]);

  if (!isOpen) return null;

  return (
    <dialog ref={dialogRef} aria-labelledby={titleId} className="broadcast-dialog font-display text-ink">
      <div onClick={onClose} className="min-h-full flex items-start justify-center px-6 py-14 box-border">
        <div
          onClick={(e) => e.stopPropagation()}
          className="w-full bg-modal border-t-4 border-ink flex flex-col"
          style={{ maxWidth }}
        >
          <div className="flex justify-between items-start gap-6 px-8 pt-7 pb-6 border-b border-line">
            <div className="flex flex-col gap-2 min-w-0">
              {kicker && <SectionLabel>{kicker}</SectionLabel>}
              <h2
                id={titleId}
                className="italic font-extrabold text-[64px] leading-tightest uppercase whitespace-nowrap overflow-hidden text-ellipsis"
              >
                {title}
              </h2>
              {meta && (
                <div className="flex flex-wrap items-center gap-x-[18px] gap-y-2 font-semibold text-[14px] tracking-t8 text-muted uppercase">
                  {meta}
                </div>
              )}
            </div>
            <GhostButton onClick={onClose} aria-label="Close dialog" className="px-3 shrink-0">
              ESC ✕
            </GhostButton>
          </div>
          {children}
        </div>
      </div>
    </dialog>
  );
};

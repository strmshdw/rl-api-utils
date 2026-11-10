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
  /** Optional extra buttons to render next to ESC ✕. */
  headerActions?: React.ReactNode;
  children: React.ReactNode;
  /** Panel max width in px (default 1180). */
  maxWidth?: number;
}

/**
 * Broadcast dialog built on the native <dialog>: full-viewport scrim, panel
 * aligned to the top with a 4px ink rule. Uses closedby="any" + pointer fallback
 * for light dismiss and overscroll-behavior: contain for scroll containment.
 */
export const Modal: React.FC<ModalProps> = ({
  isOpen,
  onClose,
  kicker,
  title,
  meta,
  headerActions,
  children,
  maxWidth = 1180,
}) => {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const pointerDownOutsideRef = useRef(false);
  const pointerDownInsideRef = useRef(false);
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
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    };
    dialog.addEventListener('cancel', handleCancel);
    dialog.addEventListener('keydown', handleKeyDown);
    return () => {
      dialog.removeEventListener('cancel', handleCancel);
      dialog.removeEventListener('keydown', handleKeyDown);
    };
  }, [onClose, isOpen]);

  // Light-dismiss pointer tracking (prevents drag-select from inside panel from closing on backdrop release)
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog || !isOpen) return;
    const isInsidePanel = (target: EventTarget | null, clientX: number, clientY: number) => {
      const panel = panelRef.current;
      if (!panel) return false;
      if (target instanceof Node && panel.contains(target)) return true;
      const rect = panel.getBoundingClientRect();
      if (rect.width === 0 && rect.height === 0) return false;
      return (
        clientX >= rect.left &&
        clientX <= rect.right &&
        clientY >= rect.top &&
        clientY <= rect.bottom
      );
    };
    const onPointerDown = (e: PointerEvent) => {
      const inside = isInsidePanel(e.target, e.clientX, e.clientY);
      pointerDownInsideRef.current = inside;
      pointerDownOutsideRef.current = !inside;
    };
    const onPointerUp = (e: PointerEvent) => {
      const inside = isInsidePanel(e.target, e.clientX, e.clientY);
      if (pointerDownOutsideRef.current && !inside) {
        pointerDownOutsideRef.current = false;
        pointerDownInsideRef.current = true;
        onClose();
        return;
      }
      pointerDownOutsideRef.current = false;
    };
    dialog.addEventListener('pointerdown', onPointerDown);
    dialog.addEventListener('pointerup', onPointerUp);
    return () => {
      dialog.removeEventListener('pointerdown', onPointerDown);
      dialog.removeEventListener('pointerup', onPointerUp);
    };
  }, [onClose, isOpen]);

  if (!isOpen) return null;

  return (
    <dialog
      ref={dialogRef}
      aria-labelledby={titleId}
      {...({ closedby: 'any' } as React.DialogHTMLAttributes<HTMLDialogElement>)}
      className="broadcast-dialog broadcast-modal font-display text-ink"
    >
      <div
        onClick={(e) => {
          if (e.target === e.currentTarget && !pointerDownInsideRef.current) {
            onClose();
          }
          pointerDownInsideRef.current = false;
        }}
        className="min-h-full flex items-start justify-center px-4 sm:px-6 py-8 sm:py-12 box-border"
      >
        <div
          ref={panelRef}
          onClick={(e) => e.stopPropagation()}
          className="w-full t-bg-modal bg-modal border-t-4 border-ink t-border t-radius flex flex-col shadow-2xl"
          style={{ maxWidth }}
        >
          <div className="flex flex-wrap justify-between items-start gap-4 px-6 pt-5 pb-4 border-b border-line t-bg-panel">
            <div className="flex flex-col gap-1.5 min-w-0">
              {kicker && <SectionLabel>{kicker}</SectionLabel>}
              <h2
                id={titleId}
                className="t-font-display italic font-extrabold text-3xl sm:text-4xl leading-none uppercase tracking-wider t-text-strong"
              >
                {title}
              </h2>
              {meta && (
                <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 font-semibold text-sm tracking-wider text-muted uppercase">
                  {meta}
                </div>
              )}
            </div>
            <div className="flex items-center gap-2 shrink-0">
              {headerActions}
              <GhostButton onClick={onClose} aria-label="Close dialog" className="px-3.5 py-1.5 text-sm shrink-0">
                ESC ✕
              </GhostButton>
            </div>
          </div>
          {children}
        </div>
      </div>
    </dialog>
  );
};


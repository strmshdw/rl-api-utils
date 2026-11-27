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
  /** Expand panel to fill the entire viewport without unmounting children (e.g. Theater mode). */
  fullViewport?: boolean;
  /** Render a compact single-row header strip (e.g. Theater mode). */
  compactHeader?: boolean;
  /** Use tighter outer vertical padding and leaner header spacing (e.g. inline 3D replay stage). */
  tightPadding?: boolean;
  /** Constrain modal height to the viewport as a flex column so children fill remaining space cleanly. */
  viewportBounded?: boolean;
  /**
   * Optional capture-phase Escape / dialog-cancel interceptor.
   * Return `true` to keep the outer modal open (the callback is responsible for handling its sub-layer).
   */
  onEscape?: (event: Event) => boolean | void;
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
  fullViewport = false,
  compactHeader = false,
  tightPadding = false,
  viewportBounded = false,
  onEscape,
}) => {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const pointerDownOutsideRef = useRef(false);
  const pointerDownInsideRef = useRef(false);
  const lastEscapeHandledAtRef = useRef(0);
  const titleId = useId();
  const onEscapeRef = useRef(onEscape);
  onEscapeRef.current = onEscape;

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

    // When a nested broadcast modal (e.g. HistoricalMatchDetailModal inside PlayerProfileDialog)
    // is open, yield Escape & cancel handling exclusively to the innermost modal.
    const hasNestedOpenModal = () =>
      Boolean(dialog.querySelector('dialog.broadcast-modal[open]'));

    // Capture-phase Escape handler runs BEFORE Shadow DOM children (CommandPaletteDialog,
    // ScopedHotkeyManager) so nested layers peel off strictly one-by-one without races.
    const handleCaptureKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (hasNestedOpenModal()) return;
      lastEscapeHandledAtRef.current = performance.now();
      if (onEscapeRef.current?.(e) === true) {
        return;
      }
      e.preventDefault();
      e.stopPropagation();
      onClose();
    };

    const handleCancel = (e: Event) => {
      e.preventDefault();
      if (hasNestedOpenModal()) return;
      // Ignore native CloseWatcher cancel firing right after capture-phase Escape already ran.
      if (performance.now() - lastEscapeHandledAtRef.current < 150) {
        return;
      }
      if (onEscapeRef.current?.(e) === true) {
        return;
      }
      onClose();
    };

    dialog.addEventListener('keydown', handleCaptureKeyDown, true);
    dialog.addEventListener('cancel', handleCancel);
    return () => {
      dialog.removeEventListener('keydown', handleCaptureKeyDown, true);
      dialog.removeEventListener('cancel', handleCancel);
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
        className={
          fullViewport
            ? 'h-full w-full flex items-stretch justify-center p-0 box-border overflow-hidden'
            : viewportBounded
            ? 'h-full w-full flex items-stretch justify-center px-3 sm:px-4 py-2 box-border overflow-hidden'
            : tightPadding
            ? 'min-h-full flex items-start justify-center px-3 sm:px-4 py-2 sm:py-3 box-border'
            : 'min-h-full flex items-start justify-center px-4 sm:px-6 py-8 sm:py-12 box-border'
        }
      >
        <div
          ref={panelRef}
          onClick={(e) => e.stopPropagation()}
          data-full-viewport={fullViewport ? 'true' : 'false'}
          data-viewport-bounded={viewportBounded ? 'true' : 'false'}
          className={
            fullViewport
              ? 'w-screen h-screen max-w-none t-bg-modal bg-modal flex flex-col overflow-hidden'
              : viewportBounded
              ? 'w-full h-full max-h-[calc(100vh-16px)] t-bg-modal bg-modal border-t-4 border-ink t-border t-radius flex flex-col shadow-2xl overflow-hidden'
              : 'w-full t-bg-modal bg-modal border-t-4 border-ink t-border t-radius flex flex-col shadow-2xl'
          }
          style={fullViewport ? undefined : { maxWidth }}
        >
          <div
            className={
              compactHeader
                ? 'flex flex-wrap justify-between items-center gap-3 px-4 py-2 border-b border-line t-bg-panel shrink-0'
                : tightPadding
                ? 'flex flex-wrap justify-between items-center gap-3 px-5 pt-2.5 pb-2 border-b border-line t-bg-panel shrink-0'
                : 'flex flex-wrap justify-between items-start gap-4 px-6 pt-5 pb-4 border-b border-line t-bg-panel'
            }
          >
            <div
              className={
                compactHeader
                  ? 'flex items-center gap-3 min-w-0'
                  : tightPadding
                  ? 'flex flex-col gap-1 min-w-0'
                  : 'flex flex-col gap-1.5 min-w-0'
              }
            >
              {!compactHeader && kicker && <SectionLabel>{kicker}</SectionLabel>}
              <h2
                id={titleId}
                className={
                  compactHeader
                    ? 't-font-display italic font-extrabold text-lg sm:text-xl leading-none uppercase tracking-wider t-text-strong truncate'
                    : tightPadding
                    ? 't-font-display italic font-extrabold text-xl sm:text-2xl leading-none uppercase tracking-wider t-text-strong truncate'
                    : 't-font-display italic font-extrabold text-3xl sm:text-4xl leading-none uppercase tracking-wider t-text-strong'
                }
              >
                {title}
              </h2>
              {!compactHeader && meta && (
                <div className="flex flex-wrap items-center gap-x-4 gap-y-1 font-semibold text-xs sm:text-sm tracking-wider text-muted uppercase">
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


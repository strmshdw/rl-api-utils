import React, { useEffect, useRef } from 'react';
import { X } from 'lucide-react';

interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
  maxWidth?: string; // e.g. "max-w-4xl"
}

export const Modal: React.FC<ModalProps> = ({
  isOpen,
  onClose,
  title,
  children,
  maxWidth = 'max-w-3xl',
}) => {
  const dialogRef = useRef<HTMLDialogElement>(null);

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

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;

    const handleCancel = (e: Event) => {
      e.preventDefault();
      onClose();
    };

    // Fallback light-dismiss for browsers lacking native closedby="any"
    const handleClick = (event: MouseEvent) => {
      if (event.target !== dialog) return;
      const rect = dialog.getBoundingClientRect();
      const inDialog =
        rect.top <= event.clientY &&
        event.clientY <= rect.top + rect.height &&
        rect.left <= event.clientX &&
        event.clientX <= rect.left + rect.width;
      if (!inDialog) {
        onClose();
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
      aria-labelledby="modal-title"
      className={`fixed inset-0 m-auto ${maxWidth} w-full rounded-2xl bg-slate-900 border border-slate-700/80 p-0 text-slate-100 shadow-2xl backdrop:bg-black/75 backdrop:backdrop-blur-sm z-50 overflow-hidden outline-none`}
    >
      <div className="flex items-center justify-between border-b border-slate-800 px-6 py-4 bg-slate-950/60">
        <h2 id="modal-title" className="text-xl font-bold tracking-tight text-white flex items-center gap-2">
          {title}
        </h2>
        <button
          onClick={onClose}
          type="button"
          aria-label="Close dialog"
          className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white transition-colors focus:ring-2 focus:ring-cyan-500 outline-none"
        >
          <X className="w-5 h-5" />
        </button>
      </div>
      <div className="p-6 max-h-[80vh] overflow-y-auto">
        {children}
      </div>
    </dialog>
  );
};

import React, { useState } from 'react';
import { Modal } from '../common/Modal';
import { PrimaryButton, GhostButton } from '../common/ui';

interface PlayerNotesModalProps {
  isOpen: boolean;
  initialNotes: string;
  initialTags: string[];
  onClose: () => void;
  onSave: (notes: string, tags: string[]) => Promise<void>;
}

const SUGGESTED_TAGS = ['smurf', 'toxic', 'reliable', 'clutch', 'freestyler', 'passer', 'party'];

export const PlayerNotesModal: React.FC<PlayerNotesModalProps> = ({
  isOpen,
  initialNotes,
  initialTags,
  onClose,
  onSave,
}) => {
  const [notes, setNotes] = useState(initialNotes);
  const [tags, setTags] = useState<string[]>(initialTags);
  const [newTagInput, setNewTagInput] = useState('');
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!isOpen) return null;

  const addTag = (tagToAdd: string) => {
    const clean = tagToAdd.trim().toLowerCase();
    if (clean && !tags.includes(clean)) {
      setTags([...tags, clean]);
    }
    setNewTagInput('');
  };

  const removeTag = (tagToRemove: string) => {
    setTags(tags.filter((t) => t !== tagToRemove));
  };

  const handleSave = async () => {
    setIsSaving(true);
    setError(null);
    try {
      await onSave(notes, tags);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save notes');
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <Modal isOpen onClose={onClose} title="EDIT SCOUTING NOTES & TAGS" kicker="PLAYER PROFILE">
      <div className="p-6 flex flex-col gap-5 font-display min-w-[340px] max-w-[500px]">
        {error && <p className="text-loss text-[14px] font-bold">{error}</p>}

        <div className="flex flex-col gap-2">
          <label className="text-[12px] font-bold tracking-wider text-muted uppercase">Tags</label>
          <div className="flex flex-wrap gap-2 items-center">
            {tags.map((t) => (
              <span
                key={t}
                data-testid={`tag-chip-${t}`}
                className="bg-panel border border-line-2 px-2.5 py-1 text-[13px] font-bold text-ink rounded flex items-center gap-1.5 uppercase"
              >
                {t}
                <button
                  type="button"
                  onClick={() => removeTag(t)}
                  className="text-muted hover:text-loss transition-colors ml-0.5"
                >
                  ×
                </button>
              </span>
            ))}
            <div className="flex items-center gap-1">
              <input
                type="text"
                value={newTagInput}
                onChange={(e) => setNewTagInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ',') {
                    e.preventDefault();
                    addTag(newTagInput);
                  }
                }}
                placeholder="ADD TAG..."
                className="bg-panel border border-line-2 px-2 py-1 text-[13px] text-ink uppercase w-28 rounded outline-none focus:border-blue-ink"
              />
              <GhostButton onClick={() => addTag(newTagInput)}>+ ADD</GhostButton>
            </div>
          </div>
          <div className="flex items-center flex-wrap gap-1 pt-1 text-[11px] text-muted">
            <span>SUGGESTIONS:</span>
            {SUGGESTED_TAGS.filter((s) => !tags.includes(s)).map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => addTag(s)}
                className="underline hover:text-ink uppercase ml-1 transition-colors"
              >
                +{s}
              </button>
            ))}
          </div>
        </div>

        <div className="flex flex-col gap-2">
          <label className="text-[12px] font-bold tracking-wider text-muted uppercase">Notes & Observations</label>
          <textarea
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            rows={4}
            placeholder="Add player notes, playstyle observations, rotation habits..."
            className="bg-panel border border-line-2 p-3 text-[14px] text-ink rounded outline-none focus:border-blue-ink font-sans resize-y"
          />
        </div>

        <div className="flex justify-end gap-3 pt-3 border-t border-line">
          <GhostButton onClick={onClose} disabled={isSaving}>
            CANCEL
          </GhostButton>
          <PrimaryButton onClick={handleSave} disabled={isSaving}>
            {isSaving ? 'SAVING…' : 'SAVE CHANGES'}
          </PrimaryButton>
        </div>
      </div>
    </Modal>
  );
};

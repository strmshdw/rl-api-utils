import React, { useEffect, useState } from 'react';
import { CurrentMatchResponse } from '../../types/api';
import { Modal } from '../common/Modal';
import { platformLabel } from '../../utils/broadcast';
import { PlayerDossierView } from './PlayerDossierView';
import { usePlayerDossierData } from './usePlayerDossierData';

export { CompactRoleSplitCard } from './PlayerRoleSplitCard';

interface PlayerProfileDialogProps {
  playerId: string | null;
  /** Name from the directory row, shown while the profile loads. */
  playerName?: string;
  onClose: () => void;
  localPlayerIds?: Set<string>;
  liveMatch?: CurrentMatchResponse | null;
  onOpenFullProfile?: (playerId: string, playerName?: string) => void;
}

const NO_IDS = new Set<string>();

export const PlayerProfileDialog: React.FC<PlayerProfileDialogProps> = ({
  playerId,
  playerName,
  onClose,
  localPlayerIds = NO_IDS,
  liveMatch,
  onOpenFullProfile,
}) => {
  const [historyStack, setHistoryStack] = useState<Array<{ id: string; name?: string }>>([]);
  const [copied, setCopied] = useState(false);

  const activeEntry = historyStack[historyStack.length - 1];
  const activeId = activeEntry?.id ?? playerId;
  const activeName = activeEntry?.name ?? playerName;

  useEffect(() => {
    setCopied(false);
    if (playerId) {
      setHistoryStack([{ id: playerId, name: playerName }]);
    } else {
      setHistoryStack([]);
    }
  }, [playerId, playerName]);

  useEffect(() => {
    const handlePopState = (event: PopStateEvent) => {
      const dialogStack = (
        event.state as { rlsyncDialogStack?: Array<{ id: string; name?: string }> } | null
      )?.rlsyncDialogStack;
      if (Array.isArray(dialogStack) && dialogStack.length > 0) {
        setHistoryStack(dialogStack);
      } else if (playerId) {
        setHistoryStack([{ id: playerId, name: playerName }]);
      }
    };
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, [playerId, playerName]);

  const dossier = usePlayerDossierData(activeId, activeName, localPlayerIds, {
    enabled: Boolean(activeId),
    pageSize: 10,
    liveMatch,
  });

  if (!activeId) return null;

  const copyId = () => {
    try {
      navigator.clipboard?.writeText(dossier.id);
    } catch {}
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handlePlayerClick = (nextPlayerId: string, nextPlayerName?: string) => {
    if (!nextPlayerId || nextPlayerId === dossier.id) return;
    setCopied(false);
    setHistoryStack((prev) => {
      const nextStack = [...prev, { id: nextPlayerId, name: nextPlayerName }];
      try {
        const curState =
          window.history.state && typeof window.history.state === 'object'
            ? window.history.state
            : {};
        window.history.pushState(
          { ...curState, rlsyncDialogStack: nextStack },
          '',
          window.location.pathname + window.location.search,
        );
      } catch {}
      return nextStack;
    });
  };

  const handleBack = () => {
    setCopied(false);
    setHistoryStack((prev) => prev.slice(0, prev.length - 1));
  };

  const kickerBadges = (
    <>
      <span
        className={`px-2 py-0.5 t-bg-page bg-page t-border border border-line text-sm font-extrabold ${dossier.relTag.color}`}
      >
        {dossier.relTag.tag}
      </span>
      {dossier.threatBadges.map((b, idx) => (
        <span
          key={idx}
          className={`px-2 py-0.5 text-sm font-extrabold uppercase tracking-wide ${b.classes}`}
        >
          {b.label}
        </span>
      ))}
    </>
  );

  return (
    <Modal
      isOpen
      onClose={onClose}
      kicker={
        historyStack.length > 1 ? (
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={handleBack}
              className="hover:underline flex items-center gap-1.5 text-blue-ink font-bold uppercase cursor-pointer"
            >
              <span>
                ← BACK TO {historyStack[historyStack.length - 2]?.name || 'PREVIOUS PLAYER'}
              </span>
            </button>
            <span className="text-muted text-sm">
              ({historyStack.map((h) => h.name || h.id).join(' → ')})
            </span>
            {kickerBadges}
          </div>
        ) : (
          <div className="flex flex-wrap items-center gap-2">
            <span>PLAYER PROFILE · {platformLabel(dossier.platform)}</span>
            {kickerBadges}
          </div>
        )
      }
      title={dossier.name}
      headerActions={
        onOpenFullProfile && (
          <button
            type="button"
            onClick={() => {
              onClose();
              onOpenFullProfile(dossier.id, dossier.name);
            }}
            className="px-3.5 py-2 t-bg-blue-slab bg-blue-slab text-white text-sm font-extrabold uppercase tracking-wider hover:opacity-90 t-radius cursor-pointer"
          >
            OPEN FULL-PAGE PROFILE →
          </button>
        )
      }
      meta={
        <>
          <span className="text-ink-2 normal-case break-all t-font-mono">{dossier.id}</span>
          <button
            type="button"
            onClick={copyId}
            className="text-muted hover:text-ink transition-colors duration-120"
          >
            {copied ? 'COPIED ✓' : 'COPY ID'}
          </button>
          {dossier.tracker && (
            <a
              href={dossier.tracker}
              target="_blank"
              rel="noopener noreferrer"
              className="text-ink hover:text-blue-ink transition-colors duration-120"
            >
              RL TRACKER ↗
            </a>
          )}
        </>
      }
    >
      <PlayerDossierView
        playerId={activeId}
        playerName={activeName}
        variant="dialog"
        localPlayerIds={localPlayerIds}
        dossier={dossier}
        liveMatch={liveMatch}
        onSelectPlayer={handlePlayerClick}
      />
    </Modal>
  );
};

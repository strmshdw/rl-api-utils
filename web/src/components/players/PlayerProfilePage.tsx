import React, { useEffect, useState } from 'react';
import { CurrentMatchResponse } from '../../types/api';
import { platformLabel } from '../../utils/broadcast';
import { PlayerDossierView } from './PlayerDossierView';
import { PlayerProfileDialog } from './PlayerProfileDialog';
import { usePlayerDossierData } from './usePlayerDossierData';

interface PlayerProfilePageProps {
  playerId: string;
  playerName?: string;
  onBack: () => void;
  localPlayerIds?: Set<string>;
  liveMatch?: CurrentMatchResponse | null;
  onOpenFullProfile?: (playerId: string, playerName?: string) => void;
}

const NO_IDS = new Set<string>();

export const PlayerProfilePage: React.FC<PlayerProfilePageProps> = ({
  playerId,
  playerName,
  onBack,
  localPlayerIds = NO_IDS,
  liveMatch,
  onOpenFullProfile,
}) => {
  const [copied, setCopied] = useState(false);
  const [quickModalPlayer, setQuickModalPlayer] = useState<{
    id: string;
    name?: string;
  } | null>(null);

  useEffect(() => {
    setCopied(false);
  }, [playerId]);

  const dossier = usePlayerDossierData(playerId, playerName, localPlayerIds, {
    enabled: Boolean(playerId),
    pageSize: 20,
    liveMatch,
  });

  const copyId = () => {
    try {
      navigator.clipboard?.writeText(dossier.id);
    } catch {}
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div data-testid="player-profile-page" className="font-display flex flex-col gap-5 pt-4">
      {/* Top Bar: Back Button, Player Identity, Relationship Tag, Copy ID & Tracker Link */}
      <div className="flex flex-wrap justify-between items-center gap-4 pb-4 border-b t-border border-line">
        <div className="flex flex-wrap items-center gap-4">
          <button
            type="button"
            data-testid="back-to-players-btn"
            onClick={onBack}
            className="px-3.5 py-2 t-bg-panel bg-panel t-border border border-outline font-extrabold text-sm t-text-blue text-blue-ink hover:text-ink-strong uppercase t-radius cursor-pointer"
          >
            ← BACK TO PLAYERS DIRECTORY
          </button>
          <div>
            <div className="flex flex-wrap items-center gap-2.5">
              <span className="text-sm font-extrabold tracking-wider text-muted uppercase">
                PLAYER PROFILE · {platformLabel(dossier.platform)} ·{' '}
                <span className="t-font-mono normal-case">{dossier.id}</span>
              </span>
              <span
                className={`px-2.5 py-0.5 t-bg-page bg-page t-border border border-line text-sm font-extrabold ${dossier.relTag.color}`}
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
              <button
                type="button"
                onClick={copyId}
                className="text-sm font-extrabold text-muted hover:text-ink uppercase cursor-pointer"
              >
                {copied ? 'COPIED ✓' : 'COPY ID'}
              </button>
              {dossier.tracker && (
                <a
                  href={dossier.tracker}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-sm font-extrabold text-ink hover:text-blue-ink uppercase"
                >
                  RL TRACKER ↗
                </a>
              )}
            </div>
            <h1 className="t-font-display italic font-extrabold text-[44px] leading-none t-text-strong text-ink-strong uppercase mt-1">
              {dossier.name}
            </h1>
          </div>
        </div>
      </div>

      <PlayerDossierView
        playerId={playerId}
        playerName={playerName}
        variant="page"
        localPlayerIds={localPlayerIds}
        dossier={dossier}
        liveMatch={liveMatch}
        onSelectPlayer={(pId, pName) => setQuickModalPlayer({ id: pId, name: pName })}
      />

      {quickModalPlayer && (
        <PlayerProfileDialog
          playerId={quickModalPlayer.id}
          playerName={quickModalPlayer.name}
          onClose={() => setQuickModalPlayer(null)}
          localPlayerIds={localPlayerIds}
          liveMatch={liveMatch}
          onOpenFullProfile={onOpenFullProfile}
        />
      )}
    </div>
  );
};

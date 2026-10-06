import React, { useState, useMemo, useEffect } from 'react';
import { useSession } from './hooks/useSession';
import { useLiveMatch } from './hooks/useLiveMatch';
import { Header } from './components/layout/Header';
import { Navbar, DashboardTab } from './components/layout/Navbar';
import { LiveGameView } from './components/live/LiveGameView';
import { SessionHistoryView } from './components/session/SessionHistoryView';
import { PlayerDirectoryView } from './components/players/PlayerDirectoryView';
import { OBSOverlayView } from './components/overlay/OBSOverlayView';

export const App: React.FC = () => {
  const [activeTab, setActiveTab] = useState<DashboardTab>('live');

  // Route / mode switch: detect OBS overlay mode via query param or path
  const isOverlayMode = useMemo(() => {
    const params = new URLSearchParams(window.location.search);
    return params.get('mode') === 'overlay' || window.location.pathname === '/overlay';
  }, []);

  useEffect(() => {
    if (isOverlayMode) {
      document.documentElement.classList.add('overlay-mode');
      document.body.classList.add('overlay-mode');
    } else {
      document.documentElement.classList.remove('overlay-mode');
      document.body.classList.remove('overlay-mode');
    }
  }, [isOverlayMode]);

  const { session, resetSession } = useSession();
  const { match } = useLiveMatch();

  // If streaming overlay mode is active, render OBS HUD exclusively
  if (isOverlayMode) {
    return <OBSOverlayView />;
  }

  const inMatch = !!match?.active_match;

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col font-sans">
      {/* Top Session & Identity Header */}
      <Header
        session={session}
        activePlaylistId={match?.playlist_id}
        onResetSession={resetSession}
        inMatch={inMatch}
      />

      {/* Navigation Tabs */}
      <Navbar
        activeTab={activeTab}
        onTabChange={setActiveTab}
        inMatch={inMatch}
        matchCount={session?.matches?.length ?? 0}
      />

      {/* Main View Area */}
      <main className={`flex-1 container mx-auto px-4 ${activeTab === 'live' ? 'py-2' : 'py-4'}`}>
        {activeTab === 'live' && <LiveGameView session={session} />}
        {activeTab === 'history' && <SessionHistoryView />}
        {activeTab === 'players' && <PlayerDirectoryView />}
      </main>

      {/* Subtle Footer (hidden during live match view to ensure zero-scroll layout) */}
      {!(activeTab === 'live' && inMatch) && (
        <footer className="border-t border-slate-900 py-3 text-center text-xs text-slate-500">
          Rocket League Play Session Dashboard &bull; Local Daemon v1.0.0
        </footer>
      )}
    </div>
  );
};

export default App;

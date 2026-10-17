import React, { createContext, useContext, useEffect, useRef } from 'react';

export type ConnectionStatus = 'connected' | 'connecting' | 'reconnecting' | 'offline';

export interface ClockUpdatePayload {
  time_remaining?: number;
  is_overtime?: boolean;
}

export type SSEEventListener<T = any> = (data: T) => void;

interface SSEManager {
  subscribe: (event: string, listener: SSEEventListener) => () => void;
  getStatus: () => ConnectionStatus;
  onStatusChange: (listener: (status: ConnectionStatus) => void) => () => void;
}

// Module-level singleton SSE manager to ensure exactly ONE EventSource is shared
class SharedSSEManager implements SSEManager {
  private es: EventSource | null = null;
  private status: ConnectionStatus = 'connecting';
  private subscribers: Map<string, Set<SSEEventListener>> = new Map();
  private statusListeners: Set<(status: ConnectionStatus) => void> = new Set();
  private reconnectTimeout: number | null = null;
  private heartbeatTimeout: number | null = null;
  private retryCount = 0;
  private subscriberCount = 0;

  constructor() {
    // Lazy initialized on first subscriber
  }

  private setStatus(newStatus: ConnectionStatus) {
    if (this.status !== newStatus) {
      this.status = newStatus;
      this.statusListeners.forEach((listener) => {
        try {
          listener(newStatus);
        } catch (e) {
          console.error('Error in status listener:', e);
        }
      });
    }
  }

  public getStatus(): ConnectionStatus {
    return this.status;
  }

  public onStatusChange(listener: (status: ConnectionStatus) => void): () => void {
    this.statusListeners.add(listener);
    listener(this.status);
    return () => {
      this.statusListeners.delete(listener);
    };
  }

  private resetHeartbeat() {
    if (this.heartbeatTimeout) {
      clearTimeout(this.heartbeatTimeout);
    }
    // Backend keepalive is every 15s; if no message arrives in 35s, reconnect
    this.heartbeatTimeout = window.setTimeout(() => {
      console.warn('SSE heartbeat timed out, reconnecting...');
      this.reconnect();
    }, 35000);
  }

  private connect() {
    if (typeof window === 'undefined' || typeof EventSource === 'undefined') {
      this.setStatus('offline');
      return;
    }

    if (this.es) {
      try {
        this.es.close();
      } catch {}
      this.es = null;
    }

    try {
      this.setStatus(this.retryCount === 0 ? 'connecting' : 'reconnecting');
      const es = new EventSource('/api/events');
      this.es = es;

      es.onopen = () => {
        this.setStatus('connected');
        this.retryCount = 0;
        this.resetHeartbeat();
      };

      es.onmessage = () => {
        this.resetHeartbeat();
      };

      const dispatch = (event: string, payload: any) => {
        this.resetHeartbeat();
        const listeners = this.subscribers.get(event);
        if (listeners) {
          listeners.forEach((fn) => {
            try {
              fn(payload);
            } catch (err) {
              console.error(`Error in SSE listener for ${event}:`, err);
            }
          });
        }
      };

      // Match update
      es.addEventListener('match_update', (e: MessageEvent) => {
        try {
          dispatch('match_update', JSON.parse(e.data));
        } catch (err) {
          console.error('Failed to parse match_update event', err);
        }
      });

      // Match ended
      es.addEventListener('match_ended', (e: MessageEvent) => {
        try {
          dispatch('match_ended', JSON.parse(e.data));
        } catch (err) {
          console.error('Failed to parse match_ended event', err);
        }
      });

      // Clock updated
      es.addEventListener('clock_updated', (e: MessageEvent) => {
        try {
          dispatch('clock_updated', JSON.parse(e.data));
        } catch (err) {
          console.error('Failed to parse clock_updated event', err);
        }
      });

      // Session update
      es.addEventListener('session_update', (e: MessageEvent) => {
        try {
          dispatch('session_update', JSON.parse(e.data));
        } catch (err) {
          console.error('Failed to parse session_update event', err);
        }
      });

      // Ranks updated
      es.addEventListener('ranks_updated', (e: MessageEvent) => {
        try {
          dispatch('ranks_updated', JSON.parse(e.data));
        } catch (err) {
          console.error('Failed to parse ranks_updated event', err);
        }
      });

      // Sync progress
      es.addEventListener('sync_progress', (e: MessageEvent) => {
        try {
          dispatch('sync_progress', JSON.parse(e.data));
        } catch (err) {
          console.error('Failed to parse sync_progress event', err);
        }
      });

      es.onerror = () => {
        this.reconnect();
      };
    } catch (err) {
      this.reconnect();
    }
  }

  private reconnect() {
    if (this.es) {
      try {
        this.es.close();
      } catch {}
      this.es = null;
    }
    if (this.heartbeatTimeout) {
      clearTimeout(this.heartbeatTimeout);
      this.heartbeatTimeout = null;
    }

    this.setStatus('reconnecting');
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
    }

    const backoff = Math.min(1000 * Math.pow(1.5, this.retryCount), 15000) + Math.random() * 500;
    this.retryCount++;

    this.reconnectTimeout = window.setTimeout(() => {
      this.connect();
    }, backoff);
  }

  public subscribe(event: string, listener: SSEEventListener): () => void {
    if (!this.subscribers.has(event)) {
      this.subscribers.set(event, new Set());
    }
    this.subscribers.get(event)!.add(listener);

    this.subscriberCount++;
    if (this.subscriberCount === 1) {
      this.connect();
    }

    return () => {
      const set = this.subscribers.get(event);
      if (set) {
        set.delete(listener);
        if (set.size === 0) {
          this.subscribers.delete(event);
        }
      }
      this.subscriberCount = Math.max(0, this.subscriberCount - 1);
      // If no active subscribers remain, close connection after 10s idle
      if (this.subscriberCount === 0) {
        window.setTimeout(() => {
          if (this.subscriberCount === 0 && this.es) {
            this.es.close();
            this.es = null;
            if (this.heartbeatTimeout) clearTimeout(this.heartbeatTimeout);
            if (this.reconnectTimeout) clearTimeout(this.reconnectTimeout);
          }
        }, 10000);
      }
    };
  }
}

export const sharedSSEManager = new SharedSSEManager();

export const SSEContext = createContext<SSEManager>(sharedSSEManager);

export const SSEProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  return <SSEContext.Provider value={sharedSSEManager}>{children}</SSEContext.Provider>;
};

export function useSSEContext(): SSEManager {
  return useContext(SSEContext) || sharedSSEManager;
}

/** Hook to subscribe to an individual SSE event on the shared connection */
export function useSSEEvent<T = any>(event: string, onData: (data: T) => void) {
  const manager = useSSEContext();
  const savedHandler = useRef(onData);

  useEffect(() => {
    savedHandler.current = onData;
  }, [onData]);

  useEffect(() => {
    const unsub = manager.subscribe(event, (data) => savedHandler.current(data));
    return unsub;
  }, [manager, event]);
}

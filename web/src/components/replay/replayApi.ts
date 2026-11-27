export type ReplaySource = 'local' | 'cache' | 'ballchasing' | 'unavailable';

export type ReplayAvailabilityReason =
  | ''
  | 'missing_ballchasing_key'
  | 'no_replay_source'
  | 'not_synced_yet'
  | 'storage_unavailable';

export type ReplayApiErrorCode =
  | 'invalid_match_guid'
  | 'match_not_found'
  | 'replay_not_synced'
  | 'replay_unavailable'
  | 'ballchasing_not_found'
  | 'ballchasing_forbidden'
  | 'ballchasing_rate_limited'
  | 'ballchasing_auth_failed'
  | 'ballchasing_upstream_error'
  | 'missing_ballchasing_api_key'
  | 'empty_replay'
  | 'network_error'
  | 'aborted'
  | 'unknown_error';

export interface ReplayStatusResponse {
  match_guid: string;
  available: boolean;
  source: ReplaySource;
  local_available: boolean;
  ballchasing_available: boolean;
  ballchasing_configured: boolean;
  ballchasing_id: string;
  ballchasing_url: string;
  filename: string;
  size_bytes: number;
  download_url: string;
  download_status: string;
  upload_status: string;
  reason: ReplayAvailabilityReason;
  message: string;
}

export interface ReplayFetchProgress {
  loadedBytes: number;
  totalBytes: number | null;
  percent: number | null;
}

export interface ReplayDownloadResult {
  bytes: Uint8Array;
  source: ReplaySource | 'local_file';
  filename: string;
  ballchasingId: string;
  matchGuid: string;
  sizeBytes: number;
}

export class ReplayApiError extends Error {
  readonly code: ReplayApiErrorCode;
  readonly status: number;
  readonly retryAfterSeconds?: number;

  constructor(
    message: string,
    code: ReplayApiErrorCode,
    status: number,
    retryAfterSeconds?: number,
  ) {
    super(message);
    this.name = 'ReplayApiError';
    this.code = code;
    this.status = status;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

export interface ReplayErrorGuidance {
  title: string;
  detail: string;
  hint: string;
  canRetry: boolean;
  canSearchBallchasing: boolean;
  retryAfterSeconds?: number;
}

export function buildReplayStatusUrl(matchGuid: string, lookup = false): string {
  const base = `/api/matches/${encodeURIComponent(matchGuid)}/replay/status`;
  return lookup ? `${base}?lookup=true` : base;
}

export function buildReplayFileUrl(matchGuid: string, download = false): string {
  const base = `/api/matches/${encodeURIComponent(matchGuid)}/replay`;
  return download ? `${base}?download=1` : base;
}

function parseRetryAfter(res: Response, bodyRetry?: unknown): number | undefined {
  if (typeof bodyRetry === 'number' && Number.isFinite(bodyRetry) && bodyRetry > 0) {
    return Math.ceil(bodyRetry);
  }
  const hdr = res.headers?.get?.('Retry-After');
  if (!hdr) return undefined;
  const num = Number(hdr);
  if (Number.isFinite(num) && num > 0) return Math.ceil(num);
  return undefined;
}

async function parseErrorResponse(res: Response): Promise<ReplayApiError> {
  let message = `HTTP ${res.status}: ${res.statusText || 'Replay request failed'}`;
  let code: ReplayApiErrorCode = 'unknown_error';
  let retryAfter: number | undefined;

  try {
    const body = (await res.json()) as {
      error?: string;
      message?: string;
      code?: string;
      retry_after_seconds?: number;
    };
    if (body?.error || body?.message) {
      message = body.error || body.message || message;
    }
    if (body?.code) {
      code = body.code as ReplayApiErrorCode;
    }
    retryAfter = parseRetryAfter(res, body?.retry_after_seconds);
  } catch {
    retryAfter = parseRetryAfter(res);
  }

  if (code === 'unknown_error') {
    if (res.status === 400) code = 'invalid_match_guid';
    else if (res.status === 403) code = 'ballchasing_forbidden';
    else if (res.status === 404) code = 'replay_unavailable';
    else if (res.status === 429) code = 'ballchasing_rate_limited';
    else if (res.status === 502) code = 'ballchasing_upstream_error';
    else if (res.status === 503) code = 'missing_ballchasing_api_key';
  }

  return new ReplayApiError(message, code, res.status, retryAfter);
}

export async function fetchReplayStatus(
  matchGuid: string,
  options?: { lookup?: boolean; signal?: AbortSignal },
): Promise<ReplayStatusResponse> {
  const url = buildReplayStatusUrl(matchGuid, Boolean(options?.lookup));
  try {
    const res = await fetch(url, { signal: options?.signal });
    if (!res.ok) {
      throw await parseErrorResponse(res);
    }
    return (await res.json()) as ReplayStatusResponse;
  } catch (err) {
    if (err instanceof ReplayApiError) throw err;
    if ((err as { name?: string })?.name === 'AbortError') {
      throw new ReplayApiError('Replay status check cancelled', 'aborted', 0);
    }
    throw new ReplayApiError(
      err instanceof Error ? err.message : 'Network error checking replay status',
      'network_error',
      0,
    );
  }
}

function normalizeReplaySource(raw: string | null): ReplaySource {
  const lower = (raw || '').trim().toLowerCase();
  if (lower === 'local' || lower === 'cache' || lower === 'ballchasing') {
    return lower;
  }
  return 'local';
}

export async function fetchReplayBinary(
  matchGuid: string,
  options?: {
    signal?: AbortSignal;
    onProgress?: (progress: ReplayFetchProgress) => void;
  },
): Promise<ReplayDownloadResult> {
  const url = buildReplayFileUrl(matchGuid, false);
  try {
    const res = await fetch(url, { signal: options?.signal });
    if (!res.ok) {
      throw await parseErrorResponse(res);
    }

    const contentLengthHeader = res.headers?.get?.('Content-Length');
    const parsedLength = contentLengthHeader ? Number(contentLengthHeader) : NaN;
    const totalBytes = Number.isFinite(parsedLength) && parsedLength > 0 ? parsedLength : null;

    let bytes: Uint8Array;
    if (res.body && typeof res.body.getReader === 'function') {
      const reader = res.body.getReader();
      const chunks: Uint8Array[] = [];
      let loadedBytes = 0;

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        if (value && value.byteLength > 0) {
          chunks.push(value);
          loadedBytes += value.byteLength;
          options?.onProgress?.({
            loadedBytes,
            totalBytes,
            percent: totalBytes ? Math.min(100, Math.round((loadedBytes / totalBytes) * 100)) : null,
          });
        }
      }

      bytes = new Uint8Array(loadedBytes);
      let offset = 0;
      for (const chunk of chunks) {
        bytes.set(chunk, offset);
        offset += chunk.byteLength;
      }
    } else {
      const buf = await res.arrayBuffer();
      bytes = new Uint8Array(buf);
      options?.onProgress?.({
        loadedBytes: bytes.byteLength,
        totalBytes: totalBytes ?? bytes.byteLength,
        percent: 100,
      });
    }

    if (bytes.byteLength === 0) {
      throw new ReplayApiError('Server returned an empty replay file.', 'empty_replay', res.status);
    }

    const source = normalizeReplaySource(res.headers?.get?.('X-Replay-Source') ?? null);
    const filename =
      res.headers?.get?.('X-Replay-Filename') || `${matchGuid}.replay`;
    const ballchasingId = res.headers?.get?.('X-Replay-Ballchasing-ID') || '';
    const resolvedGuid = res.headers?.get?.('X-Replay-Match-GUID') || matchGuid;

    return {
      bytes,
      source,
      filename,
      ballchasingId,
      matchGuid: resolvedGuid,
      sizeBytes: bytes.byteLength,
    };
  } catch (err) {
    if (err instanceof ReplayApiError) throw err;
    if ((err as { name?: string })?.name === 'AbortError') {
      throw new ReplayApiError('Replay download cancelled', 'aborted', 0);
    }
    throw new ReplayApiError(
      err instanceof Error ? err.message : 'Network error downloading replay',
      'network_error',
      0,
    );
  }
}

export function formatBytes(bytes: number | null | undefined): string {
  if (bytes == null || !Number.isFinite(bytes) || bytes < 0) return '—';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

export function describeReplayError(err: unknown): ReplayErrorGuidance {
  if (err instanceof ReplayApiError) {
    switch (err.code) {
      case 'missing_ballchasing_api_key':
        return {
          title: 'BALLCHASING API KEY NOT CONFIGURED',
          detail:
            err.message ||
            'No local replay file was found and Ballchasing downloads require an API key.',
          hint: 'Set ballchasing.api_key in config.yaml (or BALLCHASING_API_KEY) and restart RL/SYNC, or load a local .replay file below.',
          canRetry: true,
          canSearchBallchasing: false,
        };
      case 'ballchasing_auth_failed':
        return {
          title: 'BALLCHASING AUTHENTICATION FAILED',
          detail: err.message || 'Ballchasing rejected the configured API key (HTTP 401).',
          hint: 'Verify your Ballchasing API token in config.yaml or load a local .replay file below.',
          canRetry: true,
          canSearchBallchasing: false,
        };
      case 'ballchasing_forbidden':
        return {
          title: 'BALLCHASING ACCESS FORBIDDEN',
          detail: err.message || 'This replay is private or restricted on Ballchasing (HTTP 403).',
          hint: 'Use the account that uploaded the replay or load the .replay file directly from disk.',
          canRetry: false,
          canSearchBallchasing: false,
        };
      case 'ballchasing_rate_limited': {
        const wait = err.retryAfterSeconds;
        return {
          title: 'BALLCHASING RATE LIMIT REACHED',
          detail:
            wait != null
              ? `Ballchasing rate limit active. Try again in ~${wait}s.`
              : err.message || 'Ballchasing rate limit reached (HTTP 429).',
          hint: 'Wait briefly and click Retry — downloaded replays are cached locally so subsequent views are instant.',
          canRetry: true,
          canSearchBallchasing: false,
          retryAfterSeconds: wait,
        };
      }
      case 'ballchasing_not_found':
        return {
          title: 'REPLAY NOT FOUND ON BALLCHASING',
          detail: err.message || 'No matching replay file was found on Ballchasing.',
          hint: 'Sync pending replays from Rocket League or drop a local .replay file to open it directly.',
          canRetry: true,
          canSearchBallchasing: false,
        };
      case 'replay_not_synced':
      case 'replay_unavailable':
      case 'match_not_found':
        return {
          title: 'REPLAY FILE NOT YET AVAILABLE',
          detail:
            err.message ||
            'This match does not have a downloaded local .replay file or cached Ballchasing link yet.',
          hint: 'Run Sync Now to pull recent Rocket League replays, search Ballchasing, or load a local .replay file below.',
          canRetry: true,
          canSearchBallchasing: true,
        };
      case 'ballchasing_upstream_error':
        return {
          title: 'BALLCHASING SERVICE UNAVAILABLE',
          detail: err.message || 'Ballchasing returned an upstream error while fetching the replay.',
          hint: 'Retry in a moment or load a local .replay file directly.',
          canRetry: true,
          canSearchBallchasing: false,
        };
      case 'aborted':
        return {
          title: 'DOWNLOAD CANCELLED',
          detail: 'Replay loading was cancelled before completion.',
          hint: 'Click Retry to resume loading this match replay.',
          canRetry: true,
          canSearchBallchasing: false,
        };
      case 'network_error':
        return {
          title: 'NETWORK ERROR',
          detail: err.message || 'Could not reach the RL/SYNC daemon.',
          hint: 'Confirm RL/SYNC is running and click Retry, or open a local .replay file below.',
          canRetry: true,
          canSearchBallchasing: false,
        };
      default:
        return {
          title: 'REPLAY LOAD FAILED',
          detail: err.message || 'An unexpected error occurred while fetching the replay.',
          hint: 'Retry loading or open a local .replay file directly.',
          canRetry: true,
          canSearchBallchasing: false,
        };
    }
  }

  return {
    title: 'REPLAY LOAD FAILED',
    detail: err instanceof Error ? err.message : String(err || 'Unknown replay error'),
    hint: 'Retry or load a local .replay / .rlbundle file directly.',
    canRetry: true,
    canSearchBallchasing: false,
  };
}

export function describeUnavailableStatus(status: ReplayStatusResponse): ReplayErrorGuidance {
  switch (status.reason) {
    case 'missing_ballchasing_key':
      return {
        title: 'BALLCHASING API KEY REQUIRED',
        detail:
          status.message ||
          'No local replay file exists for this match and no Ballchasing API key is configured.',
        hint: 'Add your Ballchasing API key in config.yaml to download historical replays on demand, or load a local .replay file below.',
        canRetry: true,
        canSearchBallchasing: false,
      };
    case 'not_synced_yet':
      return {
        title: 'REPLAY PENDING SYNC',
        detail:
          status.message ||
          'This match has not been synced to a local .replay file yet.',
        hint: 'Click Sync Now in the top bar (or search Ballchasing if already uploaded) — or open a local .replay file below.',
        canRetry: true,
        canSearchBallchasing: status.ballchasing_configured,
      };
    case 'storage_unavailable':
      return {
        title: 'LOCAL STORAGE UNAVAILABLE',
        detail: status.message || 'Match storage is currently unavailable.',
        hint: 'Check RL/SYNC daemon status or open a local .replay file directly.',
        canRetry: true,
        canSearchBallchasing: false,
      };
    default:
      return {
        title: 'NO REPLAY FILE FOUND',
        detail:
          status.message ||
          'No local .replay file or cached Ballchasing ID is linked to this match yet.',
        hint: status.ballchasing_configured
          ? 'Search Ballchasing by match metadata to download and cache it, or load a local .replay file below.'
          : 'Configure a Ballchasing API key or drop a local .replay file below.',
        canRetry: true,
        canSearchBallchasing: status.ballchasing_configured,
      };
  }
}

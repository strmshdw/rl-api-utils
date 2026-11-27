var Ct = Object.defineProperty;
var Mt = (t, e, n) => e in t ? Ct(t, e, { enumerable: !0, configurable: !0, writable: !0, value: n }) : t[e] = n;
var ae = (t, e, n) => Mt(t, typeof e != "symbol" ? e + "" : e, n);
const Bt = "RLV4";
const O = {
  ACTIVE: 1,
  BOOST_FIRING: 2,
  SUPERSONIC: 4,
  ON_GROUND: 8,
  DODGED: 32,
  DOUBLE_JUMPED: 64,
  DEMOLISHED: 128
}, Oe = {
  HANDBRAKE: 1,
  BALL_CAM: 2,
  PICKUP_BIG: 4,
  PICKUP_SMALL: 8
}, fe = {
  OVERTIME: 1,
  BALL_HIT: 2,
  MATCH_ENDED: 4
}, j = {
  MAGIC: 0,
  ABI_VERSION: 4,
  CAR_SLOT_COUNT: 6,
  BOOST_PAD_COUNT: 7,
  FRAME_COUNT: 8,
  TICK_RATE_HZ: 12,
  TIMES_BYTE_OFFSET: 16,
  DELTAS_BYTE_OFFSET: 20,
  BALL_KIN_BYTE_OFFSET: 24,
  CAR_KIN_BYTE_OFFSET: 28,
  CAR_STATE_BYTE_OFFSET: 32,
  PAD_STATE_BYTE_OFFSET: 36,
  GAME_STATE_BYTE_OFFSET: 40,
  META_JSON_BYTE_OFFSET: 44,
  META_JSON_BYTE_LEN: 48,
  TOTAL_BYTE_LEN: 52,
  RESERVED: 56
};
function ge(t) {
  return t + 7 & -8;
}
const wt = new TextDecoder("utf-8"), Ke = /* @__PURE__ */ new Map();
function We(t) {
  if (t.byteLength < 64)
    throw new Error(`RLV4 buffer too small: ${t.byteLength} bytes`);
  const e = new DataView(t, 0, 64), n = e.getUint32(j.MAGIC, !0);
  if (n !== 878070866)
    throw new Error(`Invalid RLV4 magic: 0x${n.toString(16)}`);
  const o = e.getUint16(j.ABI_VERSION, !0);
  if (o !== 4)
    throw new Error(`Unsupported RLV4 ABI version: ${o}`);
  const i = e.getUint8(j.CAR_SLOT_COUNT), s = e.getUint8(j.BOOST_PAD_COUNT);
  if (s !== 34)
    throw new Error(`Unexpected boost pad count: ${s}`);
  const a = e.getUint32(j.FRAME_COUNT, !0), c = e.getFloat32(j.TICK_RATE_HZ, !0);
  if (!Number.isFinite(c) || c <= 0)
    throw new Error(`Invalid tickRateHz: ${c}`);
  const l = e.getUint32(j.TIMES_BYTE_OFFSET, !0), m = e.getUint32(j.DELTAS_BYTE_OFFSET, !0), f = e.getUint32(j.BALL_KIN_BYTE_OFFSET, !0), h = e.getUint32(j.CAR_KIN_BYTE_OFFSET, !0), r = e.getUint32(j.CAR_STATE_BYTE_OFFSET, !0), u = e.getUint32(j.PAD_STATE_BYTE_OFFSET, !0), p = e.getUint32(j.GAME_STATE_BYTE_OFFSET, !0), g = e.getUint32(j.META_JSON_BYTE_OFFSET, !0), _ = e.getUint32(j.META_JSON_BYTE_LEN, !0), b = e.getUint32(j.TOTAL_BYTE_LEN, !0);
  if (b % 8 !== 0 || b > t.byteLength || b < 64)
    throw new Error(`Invalid RLV4 totalByteLen=${b} (buffer=${t.byteLength})`);
  const d = [
    ["times", l, a * 4],
    ["deltas", m, a * 4],
    ["ballKin", f, a * 13 * 4],
    ["carKin", h, a * i * 13 * 4],
    ["carState", r, a * i * 8],
    ["padState", u, a * 34],
    ["gameState", p, a * 12],
    ["metaJson", g, _]
  ];
  for (const [E, v, S] of d) {
    if (v < 64 || v % 8 !== 0)
      throw new Error(`Invalid RLV4 ${E} offset=${v}: must be >= 64 and 8B-aligned`);
    if (v + S > b)
      throw new Error(`RLV4 ${E} exceeds totalByteLen (${v} + ${S} > ${b})`);
  }
  return {
    magic: Bt,
    abiVersion: o,
    carSlotCount: i,
    boostPadCount: s,
    frameCount: a,
    tickRateHz: c,
    timesByteOffset: l,
    deltasByteOffset: m,
    ballKinByteOffset: f,
    carKinByteOffset: h,
    carStateByteOffset: r,
    padStateByteOffset: u,
    gameStateByteOffset: p,
    metaJsonByteOffset: g,
    metaJsonByteLen: _,
    totalByteLen: b
  };
}
function Dt() {
  var e;
  const t = globalThis.process;
  return !!((e = t == null ? void 0 : t.versions) != null && e.node);
}
async function Pe(t) {
  const n = await (await import(
    /* @vite-ignore */
    "./__vite-browser-external-DjAKeNZ9.js"
  )).readFile(t);
  return new Uint8Array(n).slice().buffer;
}
function It() {
  const t = import.meta.url, e = /\/src\/core\/wasm\//.test(t) ? "../../../wasm/bin/rl_boxcars_v4.wasm" : /\/dist\/(?:core|workers|assets)\//.test(t) ? "../../wasm/bin/rl_boxcars_v4.wasm" : "../wasm/bin/rl_boxcars_v4.wasm";
  return new URL(e, t);
}
async function Ft(t) {
  const e = t instanceof URL ? t.toString() : t;
  if (t instanceof URL && t.protocol === "file:") {
    const { fileURLToPath: o } = await import(
      /* @vite-ignore */
      "./__vite-browser-external-DjAKeNZ9.js"
    );
    return WebAssembly.compile(await Pe(o(t)));
  }
  if (Dt() && typeof t == "string" && !/^(https?|blob|data):/.test(e)) {
    if (e.startsWith("file://")) {
      const { fileURLToPath: o } = await import(
        /* @vite-ignore */
        "./__vite-browser-external-DjAKeNZ9.js"
      );
      return WebAssembly.compile(await Pe(o(e)));
    }
    try {
      return await WebAssembly.compile(await Pe(e));
    } catch (o) {
      if (typeof fetch != "function") throw o;
    }
  }
  const n = await fetch(e);
  if (!n.ok)
    throw new Error(`Failed to fetch WASM module (${n.status}): ${e}`);
  if (typeof WebAssembly.compileStreaming == "function")
    try {
      const o = typeof n.clone == "function" ? n.clone() : n;
      return await WebAssembly.compileStreaming(o);
    } catch {
    }
  return WebAssembly.compile(await n.arrayBuffer());
}
async function Lt(t) {
  const e = t ?? It();
  if (e instanceof ArrayBuffer) return WebAssembly.compile(e);
  if (e instanceof Uint8Array) return WebAssembly.compile(e.slice().buffer);
  if (typeof e == "function") return WebAssembly.compile(await e());
  const n = e instanceof URL ? e.toString() : e;
  let o = Ke.get(n);
  return o || (o = Ft(e).catch((i) => {
    throw Ke.delete(n), i;
  }), Ke.set(n, o)), o;
}
class qe {
  constructor(e) {
    ae(this, "wasmSource");
    ae(this, "initPromise", null);
    this.wasmSource = e;
  }
  async init() {
    return this.initPromise || (this.initPromise = this.instantiateModule()), this.initPromise;
  }
  async instantiateModule() {
    const e = await Lt(this.wasmSource), o = (await WebAssembly.instantiate(e, {})).exports;
    if (!o.memory || typeof o.rlv_alloc != "function" || typeof o.rlv_parse_replay != "function")
      throw new Error("WASM module missing required RLV4 FFI exports");
    return o;
  }
  readLastError(e) {
    const n = e.rlv_last_error_ptr(), o = e.rlv_last_error_len();
    return !n || o <= 0 || n + o > e.memory.buffer.byteLength ? "Unknown WASM replay parse error" : wt.decode(new Uint8Array(e.memory.buffer, n, o));
  }
  async parseReplay(e) {
    const n = e instanceof Uint8Array ? e : new Uint8Array(e);
    if (n.byteLength === 0) throw new Error("Cannot parse empty replay byte array");
    const o = await this.init(), i = o.rlv_alloc(n.byteLength);
    if (!i) throw new Error(`WASM rlv_alloc(${n.byteLength}) returned null`);
    let s = 0, a = 0;
    try {
      new Uint8Array(o.memory.buffer, i, n.byteLength).set(n);
      const c = performance.now();
      s = o.rlv_parse_replay(i, n.byteLength);
      const l = performance.now() - c;
      if (!s) throw new Error(this.readLastError(o));
      if (s % 8 !== 0 || s + 64 > o.memory.buffer.byteLength)
        throw new Error(`Invalid WASM RLV4 header pointer: ${s}`);
      if (a = new DataView(o.memory.buffer, s, 64).getUint32(j.TOTAL_BYTE_LEN, !0), s + a > o.memory.buffer.byteLength)
        throw new Error(`RLV4 output out of WASM memory bounds (${a} bytes)`);
      const m = new Uint8Array(o.memory.buffer, s, a).slice();
      return { buffer: m.buffer, header: We(m.buffer), parseTimeMs: l };
    } finally {
      o.rlv_free(i, n.byteLength), s !== 0 && a > 0 && o.rlv_free(s, a);
    }
  }
}
const Rt = new TextDecoder("utf-8");
function xt(t, e) {
  const n = new Uint8Array(
    t,
    e.metaJsonByteOffset,
    e.metaJsonByteLen
  );
  return JSON.parse(Rt.decode(n));
}
class ke {
  constructor(e, n) {
    ae(this, "buffer");
    ae(this, "_header");
    ae(this, "_meta");
    ae(this, "_times");
    ae(this, "_deltas");
    ae(this, "_ballKin");
    ae(this, "_carKin");
    ae(this, "_carState");
    ae(this, "_padState");
    ae(this, "_gameState");
    ae(this, "_gameStateView");
    ae(this, "playlist", []);
    ae(this, "activeIndex", -1);
    ae(this, "listeners", /* @__PURE__ */ new Set());
    if (e) {
      const o = this.addMatch(e, n);
      this.selectMatch(o);
    }
  }
  static fromBuffer(e, n) {
    return new ke(e, n);
  }
  get rawBuffer() {
    return this.buffer;
  }
  get header() {
    return this._header;
  }
  get meta() {
    return this._meta;
  }
  get frameCount() {
    var e;
    return ((e = this._header) == null ? void 0 : e.frameCount) ?? 0;
  }
  get carSlotCount() {
    var e;
    return ((e = this._header) == null ? void 0 : e.carSlotCount) ?? 0;
  }
  get boostPadCount() {
    var e;
    return ((e = this._header) == null ? void 0 : e.boostPadCount) ?? 34;
  }
  get tickRateHz() {
    var e;
    return ((e = this._header) == null ? void 0 : e.tickRateHz) ?? 30;
  }
  get durationSeconds() {
    if (this._meta && this._meta.duration_seconds > 0) return this._meta.duration_seconds;
    const e = this.frameCount;
    return e > 0 ? this._times[e - 1] - this._times[0] : 0;
  }
  get times() {
    return this._times;
  }
  get deltas() {
    return this._deltas;
  }
  get ballKin() {
    return this._ballKin;
  }
  get carKin() {
    return this._carKin;
  }
  get carState() {
    return this._carState;
  }
  get padState() {
    return this._padState;
  }
  get gameState() {
    return this._gameState;
  }
  bindBuffer(e, n, o) {
    const i = n.frameCount, s = n.carSlotCount;
    this.buffer = e, this._header = n, this._meta = o, this._times = new Float32Array(e, n.timesByteOffset, i), this._deltas = new Float32Array(e, n.deltasByteOffset, i), this._ballKin = new Float32Array(e, n.ballKinByteOffset, i * 13), this._carKin = new Float32Array(e, n.carKinByteOffset, i * s * 13), this._carState = new Uint8Array(e, n.carStateByteOffset, i * s * 8), this._padState = new Uint8Array(e, n.padStateByteOffset, i * 34), this._gameState = new Uint8Array(e, n.gameStateByteOffset, i * 12), this._gameStateView = new DataView(e, n.gameStateByteOffset, i * 12);
  }
  addMatch(e, n, o) {
    const i = We(e), s = xt(e, i), a = {
      id: o ?? `${s.match_guid || "match"}-${this.playlist.length + 1}`,
      label: n ?? (s.replay_name || s.map_name || `Match ${this.playlist.length + 1}`),
      matchGuid: s.match_guid,
      frameCount: i.frameCount,
      durationSeconds: s.duration_seconds,
      buffer: e,
      header: i,
      meta: s
    }, c = this.playlist.push(a) - 1;
    return this.activeIndex === -1 && this.selectMatch(c), c;
  }
  selectMatch(e) {
    const n = this.playlist[e];
    if (!n)
      throw new RangeError(`Series match index out of range: ${e}`);
    this.activeIndex = e, this.bindBuffer(n.buffer, n.header, n.meta);
    for (const o of this.listeners) o(n, e);
    return n;
  }
  nextMatch() {
    return this.activeIndex + 1 >= this.playlist.length ? !1 : (this.selectMatch(this.activeIndex + 1), !0);
  }
  prevMatch() {
    return this.activeIndex <= 0 ? !1 : (this.selectMatch(this.activeIndex - 1), !0);
  }
  getActiveIndex() {
    return this.activeIndex;
  }
  getMatchCount() {
    return this.playlist.length;
  }
  getPlaylist() {
    return this.playlist;
  }
  getActiveMatch() {
    return this.activeIndex >= 0 ? this.playlist[this.activeIndex] ?? null : null;
  }
  onMatchChanged(e) {
    return this.listeners.add(e), () => this.listeners.delete(e);
  }
  findFrameIndex(e) {
    const n = this.frameCount;
    if (n <= 1) return 0;
    const o = this._times;
    if (e <= o[0]) return 0;
    if (e >= o[n - 1]) return n - 1;
    let i = 0, s = n - 1;
    for (; i <= s; ) {
      const a = i + s >>> 1, c = o[a];
      if (c === e) return a;
      c < e ? i = a + 1 : s = a - 1;
    }
    return Math.min(i, n - 1);
  }
  ballKinBase(e) {
    return e * 13;
  }
  carKinBase(e, n) {
    return (e * this._header.carSlotCount + n) * 13;
  }
  carStateBase(e, n) {
    return (e * this._header.carSlotCount + n) * 8;
  }
  getTime(e) {
    return this._times[e] ?? 0;
  }
  getDelta(e) {
    return this._deltas[e] ?? 0;
  }
  getCarBoostPct(e, n) {
    return this._carState[this.carStateBase(e, n)] / 255 * 100;
  }
  getCarFlagsLo(e, n) {
    return this._carState[this.carStateBase(e, n) + 1];
  }
  getCarSteer(e, n) {
    return (this._carState[this.carStateBase(e, n) + 2] - 128) / 127.5;
  }
  getCarThrottle(e, n) {
    return (this._carState[this.carStateBase(e, n) + 3] - 128) / 127.5;
  }
  getCarDemoRemMs(e, n) {
    const o = this.carStateBase(e, n);
    return this._carState[o + 4] | this._carState[o + 5] << 8;
  }
  getCarFlagsHi(e, n) {
    return this._carState[this.carStateBase(e, n) + 6];
  }
  getCarDodgeDirRaw(e, n) {
    return this._carState[this.carStateBase(e, n) + 7];
  }
  isPadActive(e, n) {
    return this._padState[e * 34 + n] === 0;
  }
  getPadCooldownSeconds(e, n) {
    const o = this._padState[e * 34 + n];
    return o === 0 ? 0 : o / 255 * (n < 6 ? 10 : 4);
  }
  getGameSecondsRemaining(e) {
    return this._gameStateView.getInt32(e * 12, !0);
  }
  getGameBlueScore(e) {
    return this._gameStateView.getUint16(e * 12 + 4, !0);
  }
  getGameOrangeScore(e) {
    return this._gameStateView.getUint16(e * 12 + 6, !0);
  }
  getGameCountdown(e) {
    return this._gameState[e * 12 + 8];
  }
  getGameFlags(e) {
    return this._gameState[e * 12 + 9];
  }
  getLastTouchSlot(e) {
    return this._gameState[e * 12 + 10];
  }
  getGamePackedShots(e) {
    return this._gameState[e * 12 + 11];
  }
}
function Ot(t, e, n, o) {
  const i = Math.hypot(t, e, n, o);
  return Math.abs(i - 1) < 1e-6 ? [t, e, n, o] : i > 1e-6 ? [t / i, e / i, n / i, o / i] : [0, 0, 0, 1];
}
function Qe(t, e, n) {
  if (t.length <= 1) return [0, 0, 0];
  const o = Math.max(0, e - 1), i = Math.min(t.length - 1, e + 1), s = t[i].time - t[o].time;
  if (s <= 1e-5) return [0, 0, 0];
  const a = n(t[o]), c = n(t[i]);
  return [(c[0] - a[0]) / s, (c[1] - a[1]) / s, (c[2] - a[2]) / s];
}
function _e(t, e, n) {
  var i, s, a, c;
  const o = (c = (a = (s = (i = t.analytics) == null ? void 0 : i.metrics) == null ? void 0 : s[e]) == null ? void 0 : a[n]) == null ? void 0 : c.value;
  return typeof o == "number" && Number.isFinite(o) ? o : null;
}
function we(t, e, n) {
  var m, f, h, r, u;
  const i = (typeof ((m = t.metadata) == null ? void 0 : m.player_slot) == "number" ? t.metadata.player_slot : null) ?? (t.player_primary ? n.get(t.player_primary) ?? null : null), a = (typeof ((f = t.metadata) == null ? void 0 : f.victim_slot) == "number" ? t.metadata.victim_slot : null) ?? (t.player_secondary ? n.get(t.player_secondary) ?? null : null), c = typeof ((h = t.metadata) == null ? void 0 : h.team) == "number" ? t.metadata.team === 1 ? 1 : 0 : null, l = typeof ((r = t.metadata) == null ? void 0 : r.details) == "string" ? t.metadata.details : t.metadata ? JSON.stringify(t.metadata) : null;
  return {
    event_type: t.event_type,
    timestamp: t.timestamp,
    frame: t.frame,
    player_slot: i,
    player_name: t.player_primary ?? null,
    victim_slot: a,
    victim_name: t.player_secondary ?? null,
    team: c ?? (i != null ? ((u = e[i]) == null ? void 0 : u.team) ?? null : null),
    details: l
  };
}
function kt(t, e) {
  var o, i;
  const n = ((o = t[e]) == null ? void 0 : o.name) ?? `Player_${e + 1}`;
  for (let s = 0; s < e; s++)
    if (((i = t[s]) == null ? void 0 : i.name) === n) return `${n} (${e + 1})`;
  return n;
}
function Nt(t) {
  const e = [], n = /* @__PURE__ */ new Map(), o = [...t.events ?? [], ...t.meta.shot_events ?? [], ...t.meta.assist_events ?? []], i = (s, a, c, l = !1) => {
    var v;
    if (!l && n.has(s)) return;
    const m = e.length, f = n.has(s) ? `${s} (${m + 1})` : s;
    n.has(s) || n.set(s, m), n.set(f, m);
    let h = 0, r = 0, u = 0, p = 0;
    for (const S of o) {
      const A = (S.event_type ?? "").toLowerCase(), M = typeof ((v = S.metadata) == null ? void 0 : v.player_slot) == "number" ? S.metadata.player_slot : null, C = M !== null ? M === m : S.player_primary === s;
      A === "goal" ? (C && h++, S.player_secondary === s && r++) : A === "assist" && C ? r++ : (A === "save" || A === "epic_save") && C ? u++ : A === "shot" && C && p++;
    }
    const g = Math.round(_e(t, f, "goals") ?? _e(t, s, "goals") ?? (c == null ? void 0 : c.goals) ?? h), _ = Math.round(_e(t, f, "assists") ?? _e(t, s, "assists") ?? (c == null ? void 0 : c.assists) ?? r), b = Math.round(_e(t, f, "saves") ?? _e(t, s, "saves") ?? (c == null ? void 0 : c.saves) ?? u), d = Math.round(_e(t, f, "shots") ?? _e(t, s, "shots") ?? (c == null ? void 0 : c.shots) ?? Math.max(g, p)), E = Math.round(_e(t, f, "score") ?? _e(t, s, "score") ?? (c == null ? void 0 : c.score) ?? g * 100 + _ * 50 + b * 50 + d * 10);
    e.push({
      slot: m,
      name: s,
      team: a,
      score: E,
      goals: g,
      assists: _,
      saves: b,
      shots: d,
      is_mvp: !!(c != null && c.is_mvp),
      is_bot: (c == null ? void 0 : c.is_bot) ?? null,
      unique_id: (c == null ? void 0 : c.unique_id) ?? null,
      body_id: (c == null ? void 0 : c.body_id) ?? null,
      hitbox_preset: (c == null ? void 0 : c.hitbox_preset) ?? "octane",
      camera_settings: (c == null ? void 0 : c.camera_settings) ?? null,
      paint: (c == null ? void 0 : c.paint) ?? null,
      ...(c == null ? void 0 : c.epic_saves) != null ? { epic_saves: c.epic_saves } : {},
      ...(c == null ? void 0 : c.clears) != null ? { clears: c.clears } : {},
      ...(c == null ? void 0 : c.centers) != null ? { centers: c.centers } : {},
      ...(c == null ? void 0 : c.aerial_hits) != null ? { aerial_hits: c.aerial_hits } : {},
      ...(c == null ? void 0 : c.crossbar_hits) != null ? { crossbar_hits: c.crossbar_hits } : {},
      ...(c == null ? void 0 : c.flip_resets) != null ? { flip_resets: c.flip_resets } : {},
      ...(c == null ? void 0 : c.first_touches) != null ? { first_touches: c.first_touches } : {}
    });
  };
  for (const s of t.meta.players ?? [])
    i(s.name, s.is_orange || s.team_id === 1 ? 1 : 0, s, !0);
  for (const s of t.time_series.frames) {
    const a = Object.keys(s.players);
    for (const c of a) i(c, e.length >= Math.ceil(a.length / 2) ? 1 : 0, void 0, !1);
  }
  return { roster: e, nameToSlot: n };
}
const Kt = new TextEncoder();
function Pt(t) {
  var L, W, P, ne;
  const e = t.time_series.frames, n = e.length, o = t.time_series.sampling_rate_hz || 30, { roster: i, nameToSlot: s } = Nt(t), a = Math.max(1, i.length), c = t.meta.teams ?? [], l = (t.meta.shot_events ?? []).map((x) => we(x, i, s)), m = (t.meta.assist_events ?? []).map((x) => we(x, i, s)), f = (L = t.meta.first_touch_events) == null ? void 0 : L.map((x) => we(x, i, s)), h = (W = t.meta.car_touch_events) == null ? void 0 : W.map((x) => we(x, i, s)), r = {
    match_guid: t.meta.match_guid || "viewer-payload",
    map_name: t.meta.map || "SoccarStandard",
    playlist: t.meta.playlist || "Standard",
    date: t.meta.date || (/* @__PURE__ */ new Date(0)).toISOString(),
    replay_name: t.meta.replay_name ?? void 0,
    team_size: t.meta.team_size ?? Math.max(1, Math.ceil(a / 2)),
    record_fps: o,
    duration_seconds: t.meta.duration_seconds || (n > 0 ? e[n - 1].time - e[0].time : 0),
    num_frames: n,
    team_0_score: ((P = c.find((x) => x.id === 0)) == null ? void 0 : P.score) ?? 0,
    team_1_score: ((ne = c.find((x) => x.id === 1)) == null ? void 0 : ne.score) ?? 0,
    players: i,
    events: (t.events ?? []).map((x) => we(x, i, s)),
    ...l.length > 0 ? { shot_events: l } : {},
    ...m.length > 0 ? { assist_events: m } : {},
    ...f !== void 0 ? { first_touch_events: f } : {},
    ...h !== void 0 ? { car_touch_events: h } : {}
  }, u = Kt.encode(JSON.stringify(r)), p = 64, g = p + ge(n * 4), _ = g + ge(n * 4), b = _ + ge(n * 13 * 4), d = b + ge(n * a * 13 * 4), E = d + ge(n * a * 8), v = E + ge(n * 34), S = v + ge(n * 12), A = S + ge(u.byteLength), M = new ArrayBuffer(A), C = new DataView(M, 0, 64);
  C.setUint32(j.MAGIC, 878070866, !0), C.setUint16(j.ABI_VERSION, 4, !0), C.setUint8(j.CAR_SLOT_COUNT, a), C.setUint8(j.BOOST_PAD_COUNT, 34), C.setUint32(j.FRAME_COUNT, n, !0), C.setFloat32(j.TICK_RATE_HZ, o, !0), C.setUint32(j.TIMES_BYTE_OFFSET, p, !0), C.setUint32(j.DELTAS_BYTE_OFFSET, g, !0), C.setUint32(j.BALL_KIN_BYTE_OFFSET, _, !0), C.setUint32(j.CAR_KIN_BYTE_OFFSET, b, !0), C.setUint32(j.CAR_STATE_BYTE_OFFSET, d, !0), C.setUint32(j.PAD_STATE_BYTE_OFFSET, E, !0), C.setUint32(j.GAME_STATE_BYTE_OFFSET, v, !0), C.setUint32(j.META_JSON_BYTE_OFFSET, S, !0), C.setUint32(j.META_JSON_BYTE_LEN, u.byteLength, !0), C.setUint32(j.TOTAL_BYTE_LEN, A, !0);
  const D = new Float32Array(M, p, n), K = new Float32Array(M, g, n), y = new Float32Array(M, _, n * 13), U = new Float32Array(M, b, n * a * 13), N = new Uint8Array(M, d, n * a * 8), H = new Uint8Array(M, E, n * 34), F = new DataView(M, v, n * 12);
  new Uint8Array(M, S, u.byteLength).set(u);
  const Y = r.events.filter((x) => (x.event_type ?? "").toLowerCase() === "goal"), Z = [...r.events.filter((x) => (x.event_type ?? "").toLowerCase() === "shot"), ...l], V = (t.time_series.channels ?? [])[3] === "yaw";
  for (let x = 0; x < n; x++) {
    const I = e[x];
    D[x] = I.time, K[x] = I.delta !== void 0 ? I.delta : x === 0 ? 1 / o : Math.max(0, I.time - e[x - 1].time);
    const G = I.ball ?? [0, 0, 92.75], w = G.length >= 17 ? [G[7], G[8], G[9]] : Qe(e, x, (B) => [B.ball[0] ?? 0, B.ball[1] ?? 0, B.ball[2] ?? 92.75]), R = G.length >= 17 ? [G[10], G[11], G[12], G[13]] : [0, 0, 0, 1];
    y.set([G[0] ?? 0, G[1] ?? 0, G[2] ?? 92.75, w[0], w[1], w[2], R[0], R[1], R[2], R[3], G[14] ?? 0, G[15] ?? 0, G[16] ?? 0], x * 13), I.pad_state && I.pad_state.length === 34 && H.set(I.pad_state, x * 34);
    const k = x * 12, Q = I.blue_score ?? (Y.length > 0 ? Y.filter((B) => B.team === 0 && B.frame <= x).length : r.team_0_score), X = I.orange_score ?? (Y.length > 0 ? Y.filter((B) => B.team === 1 && B.frame <= x).length : r.team_1_score), le = I.packed_shots !== void 0 ? I.packed_shots & 255 : Z.length > 0 ? Z.filter((B) => B.team === 0 && B.frame <= x).length & 15 | (Z.filter((B) => B.team === 1 && B.frame <= x).length & 15) << 4 : 0;
    F.setInt32(k, Math.round(I.seconds_remaining ?? Math.max(0, 300 - I.time)), !0), F.setUint16(k + 4, Q, !0), F.setUint16(k + 6, X, !0), F.setUint8(k + 8, I.countdown ?? 0), F.setUint8(k + 9, I.game_flags ?? 0), F.setUint8(k + 10, I.last_touch_slot ?? 255), F.setUint8(k + 11, le);
    for (const B of i) {
      const z = kt(i, B.slot), T = I.players[z] ?? (z === B.name ? I.players[B.name] : void 0);
      if (!T) continue;
      const J = (x * a + B.slot) * 13, te = (x * a + B.slot) * 8, $ = T.length >= 10 || !V && T.length >= 7 ? Ot(T[3] ?? 0, T[4] ?? 0, T[5] ?? 0, T[6] ?? 1) : [0, 0, Math.sin((T[3] ?? 0) * 0.5), Math.cos((T[3] ?? 0) * 0.5)], ee = T.length >= 10 ? T[7] ?? 0 : T[4] ?? 0, oe = T.length >= 10 ? T[8] ?? 0 : T[5] ?? 0, se = T.length >= 20 ? [T[10], T[11], T[12]] : Qe(e, x, (ie) => {
        const ue = ie.players[z] ?? (z === B.name ? ie.players[B.name] : void 0);
        return ue ? [ue[0] ?? 0, ue[1] ?? 0, ue[2] ?? 0] : [0, 0, 0];
      });
      if (T.length < 20 && ee > 1 && Math.hypot(se[0], se[1], se[2]) < 1e-3) {
        const ie = Math.atan2(2 * ($[3] * $[2] + $[0] * $[1]), 1 - 2 * ($[1] * $[1] + $[2] * $[2]));
        se[0] = Math.cos(ie) * ee, se[1] = Math.sin(ie) * ee;
      }
      if (U.set([T[0] ?? 0, T[1] ?? 0, T[2] ?? 17, se[0], se[1], se[2], $[0], $[1], $[2], $[3], T[13] ?? 0, T[14] ?? 0, T[15] ?? 0], J), N[te] = Math.max(0, Math.min(255, Math.round(oe / 100 * 255))), N[te + 1] = T.length >= 20 ? T[16] : O.ACTIVE | ((T[9] ?? 0) > 0 || ee >= 2200 ? O.SUPERSONIC : 0) | ((T[2] ?? 17) < 35 ? O.ON_GROUND : 0), N[te + 2] = T.length >= 20 ? T[18] : 128, N[te + 3] = T.length >= 20 ? T[19] : 128, T.length >= 22) {
        const ie = Math.max(0, Math.min(65535, Math.round(T[20] ?? 0)));
        N[te + 4] = ie & 255, N[te + 5] = ie >>> 8 & 255, N[te + 7] = Math.max(0, Math.min(255, Math.round(T[21] ?? 0)));
      }
      N[te + 6] = T.length >= 20 ? T[17] : Oe.BALL_CAM;
    }
  }
  return M;
}
function ze(t, e = 1 / Math.max(1, t.tickRateHz || 30)) {
  const n = t.frameCount, o = new Uint8Array(n), i = new Float32Array(n);
  if (n === 0)
    return { isFrameActive: o, dtByFrame: i, activeMatchDuration: 0, hasCountdowns: !1, hasBallHitFlag: !1 };
  let s = !1, a = !1;
  for (let r = 0; r < n; r++)
    t.getGameCountdown(r) > 0 && (s = !0), (t.getGameFlags(r) & fe.BALL_HIT) !== 0 && (a = !0);
  const c = /* @__PURE__ */ new Set();
  for (const r of t.meta.events)
    (r.event_type ?? "").toLowerCase() === "goal" && r.frame >= 0 && r.frame < n && c.add(r.frame);
  let l = !s, m = -999, f = 0;
  const h = t.ballKin;
  for (let r = 0; r < n; r++) {
    const u = t.getDelta(r), p = u > 1e-4 && u < 0.25 ? u : e;
    i[r] = p;
    const g = t.getGameCountdown(r), _ = r > 0 ? t.getGameCountdown(r - 1) : 0, b = t.getGameFlags(r), d = (b & fe.MATCH_ENDED) !== 0, E = r > 0 && (b & fe.OVERTIME) !== 0 && (t.getGameFlags(r - 1) & fe.OVERTIME) === 0 && (b & fe.BALL_HIT) === 0;
    if (d || g > 0 || E)
      l = !1;
    else if (_ > 0 && g === 0)
      l = !0;
    else if (!l && s && m < 0 && (b & fe.BALL_HIT) !== 0)
      l = !0;
    else if (!l && !s && m >= 0 && (t.getTime(r) - t.getTime(m) > 0.08 || r > m + 3)) {
      const v = t.ballKinBase(r);
      Math.hypot(h[v], h[v + 1]) < 100 && Math.hypot(h[v + 3], h[v + 4]) < 80 && (l = !0);
    }
    l && (o[r] = 1, f += p, c.has(r) && (l = !1, m = r));
  }
  return { isFrameActive: o, dtByFrame: i, activeMatchDuration: f, hasCountdowns: s, hasBallHitFlag: a };
}
class Ut {
  static analyze(e, n) {
    var A, M, C, D, K;
    const o = e.frameCount, i = e.carSlotCount, { isFrameActive: s, dtByFrame: a, activeMatchDuration: c, hasCountdowns: l, hasBallHitFlag: m } = ze(e, n), f = new Int8Array(o).fill(-1), h = new Float32Array(o), r = new Float32Array(o), u = [], p = [], g = [];
    if (o === 0)
      return {
        isFrameActive: s,
        dtByFrame: a,
        activeMatchDuration: 0,
        hasCountdowns: !1,
        possessingTeam: f,
        blueAvgBoost: h,
        orangeAvgBoost: r,
        touches: u,
        team0ThreatTimes: p,
        team1ThreatTimes: g
      };
    const _ = e.meta.players, b = (A = e.meta.shot_events) != null && A.length ? [...e.meta.events, ...e.meta.shot_events] : e.meta.events;
    for (const y of b) {
      const U = (y.event_type ?? "").toLowerCase();
      if (U === "goal" || U === "shot") {
        const N = y.player_slot != null ? _[y.player_slot] : _.find((F) => F.name === y.player_name), H = y.team ?? (N == null ? void 0 : N.team);
        H === 0 ? p.push(y.timestamp) : H === 1 && g.push(y.timestamp);
      }
    }
    const d = e.ballKin;
    let E = -1;
    const v = e.carKin, S = new Float64Array(Math.max(1, i)).fill(-999);
    for (let y = 0; y < o; y++) {
      let U = 0, N = 0, H = 0, F = 0;
      for (let V = 0; V < i; V++) {
        const L = e.getCarFlagsLo(y, V);
        if ((L & O.ACTIVE) === 0 || (L & O.DEMOLISHED) !== 0) continue;
        const W = e.getCarBoostPct(y, V);
        (((M = _[V]) == null ? void 0 : M.team) ?? 0) === 1 ? (H += W, F++) : (U += W, N++);
      }
      h[y] = N > 0 ? U / N : 100, r[y] = F > 0 ? H / F : 100;
      const Y = e.getGameFlags(y);
      if (s[y] === 0 || e.getGameCountdown(y) > 0 || m && (Y & fe.BALL_HIT) === 0) {
        E = -1, f[y] = -1;
        continue;
      }
      const Z = e.getLastTouchSlot(y);
      if (Z < i && _[Z] && (E = _[Z].team === 1 ? 1 : 0), y > 0) {
        const V = a[y], L = e.getTime(y), W = e.ballKinBase(y - 1), P = e.ballKinBase(y), ne = (d[P] - d[W]) ** 2 + (d[P + 1] - d[W + 1]) ** 2 + (d[P + 2] - d[W + 2]) ** 2;
        let x = Math.hypot(d[P + 3] - d[W + 3], d[P + 4] - d[W + 4], d[P + 5] - (d[W + 5] - 650 * V));
        if (x < 180 && V < 0.025) {
          let I = y - 1;
          for (; I > 0 && s[I] !== 0 && L - e.getTime(I - 1) <= 0.036; ) {
            const G = e.ballKinBase(I - 1), w = e.ballKinBase(I);
            if ((d[w] - d[G]) ** 2 + (d[w + 1] - d[G + 1]) ** 2 + (d[w + 2] - d[G + 2]) ** 2 > 1500 * 1500) break;
            I--;
          }
          if (I < y - 1) {
            const G = e.ballKinBase(I), w = Math.max(V, L - e.getTime(I)), R = Math.hypot(d[P + 3] - d[G + 3], d[P + 4] - d[G + 4], d[P + 5] - (d[G + 5] - 650 * w));
            R > x && (x = R);
          }
        }
        if (x >= 180 && ne <= 2500 * 2500) {
          let I = -1, G = 260, w = -1 / 0, R = -1;
          for (let k = 0; k < i; k++) {
            const Q = e.getCarFlagsLo(y, k);
            if ((Q & O.ACTIVE) === 0 || (Q & O.DEMOLISHED) !== 0) continue;
            const X = e.carKinBase(y, k), le = d[P] - v[X], B = d[P + 1] - v[X + 1], z = d[P + 2] - v[X + 2], T = Math.hypot(le, B, z), J = (v[X + 3] * le + v[X + 4] * B + v[X + 5] * z) / Math.max(1, T);
            T < G - 1e-3 ? (G = T, I = k, w = J, R = -1) : Math.abs(T - G) <= 1e-3 && (k === Z ? (G = T, I = k, w = J, R = -1) : I < 0 ? (G = T, I = k, w = J) : I !== Z && ((((C = _[k]) == null ? void 0 : C.team) ?? 0) !== (((D = _[I]) == null ? void 0 : D.team) ?? 0) ? R = k : J > w + 1e-3 && (I = k, w = J)));
          }
          if (I >= 0 && R >= 0) {
            const k = d[P + 4], Q = (((K = _[I]) == null ? void 0 : K.team) ?? 0) === 1;
            k > 1 ? I = Q ? R : I : k < -1 ? I = Q ? I : R : (I = -1, E = -1);
          }
          if (I >= 0 && _[I]) {
            const k = _[I].team === 1 ? 1 : 0;
            if (E = k, L - S[I] >= 0.18) {
              S[I] = L, u.push({
                frame: y,
                time: L,
                slot: I,
                team: k,
                x: d[P],
                y: d[P + 1],
                z: d[P + 2],
                vx: d[P + 3],
                vy: d[P + 4],
                vz: d[P + 5],
                impulse: x
              });
              const Q = k === 0 ? 1 : -1;
              d[P + 1] * Q > 2600 && Math.abs(d[P]) <= 1200 && d[P + 4] * Q > 750 && (k === 0 ? p.push(L) : g.push(L));
            }
          }
        }
      }
      f[y] = E;
    }
    return p.sort((y, U) => y - U), g.sort((y, U) => y - U), {
      isFrameActive: s,
      dtByFrame: a,
      activeMatchDuration: c,
      hasCountdowns: l,
      possessingTeam: f,
      blueAvgBoost: h,
      orangeAvgBoost: r,
      touches: u,
      team0ThreatTimes: p,
      team1ThreatTimes: g
    };
  }
}
const Re = 5120 / 3, Vt = 643, Gt = 2300, Ht = 1410, Yt = 2200, $t = 2100, Wt = 1, me = {
  goal: 1,
  epic_save: 0.7,
  save: 0.55,
  assist: 0.45,
  shot: 0.25,
  demo: 0.35,
  demo_taken: 0.3
};
class He {
  constructor(e = 300, n = 12) {
    ae(this, "duration");
    ae(this, "viewStart", 0);
    ae(this, "viewEnd");
    ae(this, "minWindowSeconds", 5);
    ae(this, "decayHalfLifeSeconds");
    this.decayHalfLifeSeconds = Math.max(0.5, n), this.duration = Math.max(1, e), this.viewEnd = this.duration;
  }
  setDecayHalfLife(e) {
    this.decayHalfLifeSeconds = Math.max(0.5, e);
  }
  getDecayHalfLife() {
    return this.decayHalfLifeSeconds;
  }
  getMaxZoomFactor() {
    return this.duration / this.minWindowSeconds;
  }
  getDuration() {
    return this.duration;
  }
  setDuration(e) {
    this.duration = Math.max(1, e), this.viewStart = 0, this.viewEnd = this.duration;
  }
  resetZoom() {
    this.viewStart = 0, this.viewEnd = this.duration;
  }
  getWindow() {
    const e = Math.max(1e-3, this.viewEnd - this.viewStart);
    return { start: this.viewStart, end: this.viewEnd, duration: e, zoomFactor: this.duration / e };
  }
  setWindow(e, n) {
    const o = Math.min(this.minWindowSeconds, this.duration);
    let i = Math.max(0, Math.min(this.duration - o, e)), s = Math.min(this.duration, Math.max(i + o, n));
    s - i < o && (s = Math.min(this.duration, i + o), i = Math.max(0, s - o)), this.viewStart = i, this.viewEnd = s;
  }
  zoomAt(e, n = 0.5) {
    const o = this.viewEnd - this.viewStart, i = Math.max(0, Math.min(1, n)), s = this.viewStart + o * i, a = Math.min(this.minWindowSeconds, this.duration), c = Math.max(a, Math.min(this.duration, o * e));
    let l = s - c * i, m = l + c;
    l < 0 ? (l = 0, m = Math.min(this.duration, c)) : m > this.duration && (m = this.duration, l = Math.max(0, this.duration - c)), this.viewStart = l, this.viewEnd = m;
  }
  pan(e) {
    const n = this.viewEnd - this.viewStart;
    let o = this.viewStart + e, i = this.viewEnd + e;
    o < 0 ? (o = 0, i = n) : i > this.duration && (i = this.duration, o = Math.max(0, this.duration - n)), this.viewStart = o, this.viewEnd = i;
  }
  focusTime(e, n) {
    const o = Math.min(this.minWindowSeconds, this.duration), i = n ? Math.max(o, Math.min(this.duration, n)) : this.viewEnd - this.viewStart;
    let s = e - i / 2, a = e + i / 2;
    s < 0 ? (s = 0, a = Math.min(this.duration, i)) : a > this.duration && (a = this.duration, s = Math.max(0, this.duration - i)), this.viewStart = s, this.viewEnd = a;
  }
  timeToPixel(e, n) {
    return (e - this.viewStart) / Math.max(1e-3, this.viewEnd - this.viewStart) * n;
  }
  pixelToTime(e, n) {
    return this.viewStart + Math.max(0, Math.min(1, e / Math.max(1, n))) * Math.max(1e-3, this.viewEnd - this.viewStart);
  }
  valueToPixel(e, n, o = 1, i = 12) {
    const s = n / 2;
    return s - e / Math.max(1e-3, o) * Math.max(1, s - i);
  }
  timeToOverviewPixel(e, n) {
    return Math.max(0, Math.min(1, e / this.duration)) * n;
  }
  getOverviewBrushRange(e) {
    const n = this.timeToOverviewPixel(this.viewStart, e), o = this.timeToOverviewPixel(this.viewEnd, e);
    return { startX: n, endX: o, width: Math.max(4, o - n) };
  }
  calculateDecayedMomentumAtTime(e, n, o = this.decayHalfLifeSeconds) {
    const i = Math.LN2 / Math.max(0.1, o);
    let s = 0, a = 0;
    for (const c of n) {
      if (c.timestamp > e) break;
      s = s * Math.exp(-i * Math.max(0, c.timestamp - a)) + c.pva_delta, a = c.timestamp;
    }
    return e > a ? s * Math.exp(-i * (e - a)) : s;
  }
  sampleMomentumCurve(e, n, o, i, s = 1, a = !1) {
    const l = [...a ? n : n.filter((h) => h.tag !== "DEMO_TAKEN")].sort((h, r) => h.timestamp - r.timestamp), m = Math.max(10, e), f = [];
    for (let h = 0; h <= m; h++) {
      const r = this.viewStart + h / m * (this.viewEnd - this.viewStart), u = this.calculateDecayedMomentumAtTime(r, l);
      f.push({ time: r, value: u, x: this.timeToPixel(r, o), y: this.valueToPixel(u, i, s) });
    }
    return f;
  }
  samplePlayerLayerCurves(e, n, o, i, s = 1) {
    var l;
    const a = {};
    for (const m of n)
      m.player && (a[l = m.player] ?? (a[l] = [])).push(m);
    const c = {};
    for (const [m, f] of Object.entries(a))
      c[m] = this.sampleMomentumCurve(e, f, o, i, s, !0);
    return c;
  }
  calculateStatsSummary(e) {
    const n = e.filter((f) => f.tag !== "DEMO_TAKEN");
    if (n.length === 0)
      return { blueDominancePct: 50, orangeDominancePct: 50, volatilityIndex: 0, bluePeak: null, orangePeak: null };
    let o = 0, i = 0, s = 0, a = null, c = null;
    for (const f of n)
      s += Math.abs(f.pva_delta), f.pva_delta > 0 ? (o += f.pva_delta, (!a || f.pva_delta > a.pva_delta) && (a = f)) : f.pva_delta < 0 && (i += Math.abs(f.pva_delta), (!c || f.pva_delta < c.pva_delta) && (c = f));
    const l = o + i, m = l > 0 ? Math.round(o / l * 100) : 50;
    return { blueDominancePct: m, orangeDominancePct: 100 - m, volatilityIndex: Number((s / Math.max(1, this.duration)).toFixed(3)), bluePeak: a, orangePeak: c };
  }
  static extractMomentsFromStore(e) {
    var m, f, h;
    const n = [], o = e.meta.players, { isFrameActive: i, hasCountdowns: s } = ze(e), a = [
      ...e.meta.events,
      ...e.meta.shot_events ?? [],
      ...e.meta.assist_events ?? []
    ];
    for (const r of a) {
      const u = (r.event_type ?? "").toLowerCase(), p = u === "demo" || u === "demolition";
      if (p && s && r.frame >= 0 && r.frame < i.length && i[r.frame] === 0) continue;
      const g = r.player_slot != null ? o[r.player_slot] : o.find((d) => d.name === r.player_name), _ = r.team ?? (g == null ? void 0 : g.team) ?? 0, b = u === "goal" ? me.goal : u === "epic_save" ? me.epic_save : u === "save" ? me.save : u === "assist" ? me.assist : p ? me.demo : u === "shot" ? me.shot : 0.25;
      if (n.push({
        timestamp: r.timestamp,
        player: r.player_name ?? (g == null ? void 0 : g.name) ?? "Unknown",
        team: _ === 1 ? "Orange" : "Blue",
        pva_delta: (_ === 1 ? -1 : 1) * b,
        tag: r.event_type.toUpperCase(),
        description: `${r.event_type} by ${r.player_name ?? (g == null ? void 0 : g.name) ?? "Unknown"}`
      }), p && (r.victim_name || r.victim_slot != null)) {
        const d = r.victim_slot != null ? o[r.victim_slot] : o.find((v) => v.name === r.victim_name), E = (d == null ? void 0 : d.team) ?? (_ === 1 ? 0 : 1);
        n.push({
          timestamp: r.timestamp,
          player: r.victim_name ?? (d == null ? void 0 : d.name) ?? "Unknown",
          team: E === 1 ? "Orange" : "Blue",
          pva_delta: (E === 1 ? 1 : -1) * me.demo_taken,
          tag: "DEMO_TAKEN",
          description: `Demolished by ${r.player_name ?? (g == null ? void 0 : g.name) ?? "Opponent"}`
        });
      }
    }
    let c = -1;
    const l = e.ballKin;
    for (let r = 1; r < e.frameCount; r++) {
      if (s && i[r] === 0 || e.getGameCountdown(r) > 0 || (e.getGameFlags(r) & fe.MATCH_ENDED) !== 0) continue;
      const u = e.getTime(r), p = u - e.getTime(r - 1);
      if (u - c < 0.75 || !(p > 1e-4 && p < 0.25)) continue;
      const g = e.ballKinBase(r - 1), _ = e.ballKinBase(r), b = (l[_] - l[g]) ** 2 + (l[_ + 1] - l[g + 1]) ** 2 + (l[_ + 2] - l[g + 2]) ** 2, d = Math.hypot(l[_ + 3] - l[g + 3], l[_ + 4] - l[g + 4], l[_ + 5] - (l[g + 5] - 650 * p));
      if (d < 450 || b > 2500 * 2500) continue;
      const E = e.getLastTouchSlot(r);
      let v = -1, S = 250, A = -1;
      for (let C = 0; C < e.carSlotCount; C++) {
        const D = e.getCarFlagsLo(r, C);
        if ((D & O.ACTIVE) === 0 || (D & O.DEMOLISHED) !== 0) continue;
        const K = e.carKinBase(r, C), y = Math.hypot(e.carKin[K] - l[_], e.carKin[K + 1] - l[_ + 1], e.carKin[K + 2] - l[_ + 2]);
        y < S - 1e-3 ? (S = y, v = C, A = -1) : Math.abs(y - S) <= 1e-3 && (C === E || v < 0 ? (S = y, v = C, A = -1) : v !== E && (((m = o[C]) == null ? void 0 : m.team) ?? 0) !== (((f = o[v]) == null ? void 0 : f.team) ?? 0) && (A = C));
      }
      if (v >= 0 && A >= 0) {
        const C = l[_ + 4], D = (((h = o[v]) == null ? void 0 : h.team) ?? 0) === 1;
        C > 1 ? v = D ? A : v : C < -1 ? v = D ? v : A : v = -1;
      }
      if (v < 0 || !o[v]) continue;
      const M = o[v];
      n.push({
        timestamp: u,
        player: M.name,
        team: M.team === 1 ? "Orange" : "Blue",
        pva_delta: Number(((M.team === 0 ? 1 : -1) * Math.min(0.35, d / 3200)).toFixed(3)),
        tag: "TOUCH",
        description: `High-energy ball touch (${Math.round(d)} UU/s)`
      }), c = u;
    }
    return n.sort((r, u) => r.timestamp - u.timestamp);
  }
  static extractPlayerPeaksAndDips(e) {
    var o;
    const n = {};
    for (const i of e) {
      if (!i.player) continue;
      const s = n[o = i.player] ?? (n[o] = { peak: null, dip: null }), a = i.team.toLowerCase() === "orange" ? -1 : 1, c = i.pva_delta * a;
      (!s.peak || c > s.peak.pva_delta * (s.peak.team.toLowerCase() === "orange" ? -1 : 1)) && (s.peak = i), (!s.dip || c < s.dip.pva_delta * (s.dip.team.toLowerCase() === "orange" ? -1 : 1)) && (s.dip = i);
    }
    return n;
  }
}
const zt = 208, Zt = 144, Jt = 160, qt = 120, Qt = 10, Xt = 4, jt = 100, en = 12;
function q(t, e, n, o) {
  return {
    padIndex: t,
    pos: [e, n, o ? 73 : 70],
    isBig: o,
    radius: o ? zt : Zt,
    height: o ? Jt : qt,
    respawnSeconds: o ? Qt : Xt,
    boostAmount: o ? jt : en
  };
}
const Xe = [
  // 0..5: 6 Big Pads (100 boost, R=208 UU, 10s respawn, Z=73 UU)
  q(0, -3072, -4096, !0),
  q(1, 3072, -4096, !0),
  q(2, -3584, 0, !0),
  q(3, 3584, 0, !0),
  q(4, -3072, 4096, !0),
  q(5, 3072, 4096, !0),
  // 6..33: 28 Small Pads (12 boost, R=144 UU, 4s respawn, Z=70 UU)
  q(6, 0, -4240, !1),
  q(7, -1792, -4184, !1),
  q(8, 1792, -4184, !1),
  q(9, -940, -3308, !1),
  q(10, 940, -3308, !1),
  q(11, 0, -2816, !1),
  q(12, -3584, -2484, !1),
  q(13, 3584, -2484, !1),
  q(14, -1788, -2302, !1),
  q(15, 1788, -2302, !1),
  q(16, -2048, -1036, !1),
  q(17, 2048, -1036, !1),
  q(18, 0, -1024, !1),
  q(19, -1024, 0, !1),
  q(20, 1024, 0, !1),
  q(21, 0, 1024, !1),
  q(22, -2048, 1036, !1),
  q(23, 2048, 1036, !1),
  q(24, -1788, 2302, !1),
  q(25, 1788, 2302, !1),
  q(26, -3584, 2484, !1),
  q(27, 3584, 2484, !1),
  q(28, 0, 2816, !1),
  q(29, -940, 3308, !1),
  q(30, 940, 3308, !1),
  q(31, -1792, 4184, !1),
  q(32, 1792, 4184, !1),
  q(33, 0, 4240, !1)
], tn = 400 * 400, Ye = 205, nn = 0.9659258;
function on(t, e, n = 17) {
  if (n > Ye) return null;
  let o = null, i = tn;
  for (let s = 0; s < Xe.length; s++) {
    const a = Xe[s], c = (t - a.pos[0]) ** 2 + (e - a.pos[1]) ** 2;
    c <= i && (i = c, o = a);
  }
  return o;
}
class be {
  static createInitialState() {
    return {
      boostTimeIntegral: 0,
      activeTime: 0,
      totalBoostConsumed: 0,
      totalBoostCollected: 0,
      amountStolen: 0,
      amountCollectedBig: 0,
      amountStolenBig: 0,
      amountCollectedSmall: 0,
      amountStolenSmall: 0,
      countCollectedBig: 0,
      countStolenBig: 0,
      countCollectedSmall: 0,
      countStolenSmall: 0,
      smoothSmallPadCount: 0,
      amountOverfill: 0,
      amountOverfillStolen: 0,
      amountUsedSupersonic: 0,
      kineticEnergyGainKuu: 0,
      starvationTimeInflicted: 0,
      timeZeroBoost: 0,
      timeFullBoost: 0,
      timeBoost0_25: 0,
      timeBoost25_50: 0,
      timeBoost50_75: 0,
      timeBoost75_100: 0,
      prevBoost: 33.3,
      prevSpeed: 0,
      hasPrev: !1,
      prevBoostFiring: !1,
      simTime: 0,
      lastCountedPickupTime: -999,
      padAvailAt: new Float64Array(34)
    };
  }
  static resetContinuity(e) {
    e.hasPrev = !1, e.prevBoostFiring = !1;
  }
  static processSample(e, n, o, i, s, a, c, l = 0, m = !0, f = !1, h = 17, r = !1, u, p, g = 0) {
    e.boostTimeIntegral += n * a, e.activeTime += a, e.simTime = p ?? e.simTime + a, f && (e.starvationTimeInflicted += a);
    let _ = 0;
    if (e.hasPrev) {
      const b = e.prevBoost, d = on(o, i, h), E = !r && Math.abs(n - 100 * (85 / 255)) < 0.45 && (b < 15 && d === null || b - n > 15), v = u ?? e.padAvailAt, S = e.simTime - e.lastCountedPickupTime >= 0.4, A = d ? e.simTime >= v[d.padIndex] : S, M = g !== 0 && n >= 95 && b >= 94 && n >= b - 1.5 && A && S;
      if (!E && !M && n < b - 0.05) {
        const C = b - n;
        _ = C, e.totalBoostConsumed += C, c && (e.amountUsedSupersonic += C);
        const D = 0.5 * (l * l - e.prevSpeed * e.prevSpeed);
        D > 0 && (e.kineticEnergyGainKuu += D / 1e3);
      } else if (!E && (n > b + 0.05 || M)) {
        const C = e.prevBoostFiring && b > 0 && a > 0 && a < 0.25 ? Math.min(b, 33.333333333333336 * a) : 0, D = b - C, K = Math.max(0, n - D), y = n >= 95.5 && A && (g === 2 || g === 0 && (d !== null ? d.isBig : h <= Ye && K > 16)), U = !y && A && (g === 1 && S || g === 0 && (d !== null ? !d.isBig && K >= 1 : h <= Ye && (K >= 4 || n >= 99 && D < 98.5)));
        if (y || U) {
          if (e.lastCountedPickupTime = e.simTime, C > 0) {
            _ = C, e.totalBoostConsumed += C, c && (e.amountUsedSupersonic += C);
            const F = 0.5 * (l * l - e.prevSpeed * e.prevSpeed);
            F > 0 && (e.kineticEnergyGainKuu += F / 1e3);
          }
          d && (v[d.padIndex] = e.simTime + (d.isBig ? 9.6 : 3.7));
          const N = d ? d.pos[1] : i, H = s ? N < 0 : N > 0;
          if (y) {
            const F = Math.min(100, K), Y = Math.max(0, D);
            e.totalBoostCollected += F, e.countCollectedBig++, e.amountCollectedBig += F, e.amountOverfill += Y, H && (e.countStolenBig++, e.amountStolenBig += F, e.amountStolen += F, e.amountOverfillStolen += Y);
          } else {
            const F = Math.min(12, K), Y = Math.max(0, D + 12 - 100);
            e.totalBoostCollected += F, e.countCollectedSmall++, e.amountCollectedSmall += F, e.amountOverfill += Y, m && e.smoothSmallPadCount++, H && (e.countStolenSmall++, e.amountStolenSmall += F, e.amountStolen += F, e.amountOverfillStolen += Y);
          }
        }
      }
    }
    return e.prevBoost = n, e.prevSpeed = l, e.hasPrev = !0, e.prevBoostFiring = r, n < 0.5 ? e.timeZeroBoost += a : n >= 99.5 && (e.timeFullBoost += a), n <= 25 ? e.timeBoost0_25 += a : n <= 50 ? e.timeBoost25_50 += a : n <= 75 ? e.timeBoost50_75 += a : e.timeBoost75_100 += a, _;
  }
  static isSmoothHeadingAlignment(e, n, o, i, s, a, c) {
    if (c) return !1;
    const l = Math.hypot(e, n);
    if (l < 200) return !0;
    const m = 1 - 2 * (i * i + s * s), f = 2 * (o * i + a * s), h = Math.hypot(m, f);
    return h < 1e-4 ? !0 : Math.abs(e * m + n * f) / (l * h) >= nn;
  }
  static finalize(e, n) {
    const o = e.activeTime > 1e-4 ? e.activeTime : Math.max(1e-3, n), i = e.activeTime > 1e-4 ? e.totalBoostConsumed / o * 60 : 0, s = e.activeTime > 1e-4 ? e.totalBoostCollected / o * 60 : 0, a = e.activeTime > 1e-4 ? e.boostTimeIntegral / e.activeTime : 0, c = Math.round(e.amountCollectedBig), l = Math.round(e.amountCollectedSmall), m = Math.round(e.amountStolenBig), f = Math.round(e.amountStolenSmall);
    return {
      bpm: Number(i.toFixed(1)),
      bcpm: Number(s.toFixed(1)),
      avg_amount: Number(a.toFixed(1)),
      amount_collected: c + l,
      amount_stolen: m + f,
      amount_collected_big: c,
      amount_stolen_big: m,
      amount_collected_small: l,
      amount_stolen_small: f,
      count_collected_big: e.countCollectedBig,
      count_stolen_big: e.countStolenBig,
      count_collected_small: e.countCollectedSmall,
      count_stolen_small: e.countStolenSmall,
      amount_overfill: Math.round(e.amountOverfill),
      amount_overfill_stolen: Math.round(e.amountOverfillStolen),
      amount_used_supersonic: Math.round(e.amountUsedSupersonic),
      time_zero_boost: Number(e.timeZeroBoost.toFixed(1)),
      percent_zero_boost: e.activeTime > 1e-4 ? Number((e.timeZeroBoost / o * 100).toFixed(1)) : 0,
      time_full_boost: Number(e.timeFullBoost.toFixed(1)),
      percent_full_boost: e.activeTime > 1e-4 ? Number((e.timeFullBoost / o * 100).toFixed(1)) : 0,
      time_boost_0_25: Number(e.timeBoost0_25.toFixed(1)),
      time_boost_25_50: Number(e.timeBoost25_50.toFixed(1)),
      time_boost_50_75: Number(e.timeBoost50_75.toFixed(1)),
      time_boost_75_100: Number(e.timeBoost75_100.toFixed(1))
    };
  }
}
function an(t) {
  var o, i;
  if (t.meta.car_touch_events === void 0) return null;
  const e = /* @__PURE__ */ new Map(), n = t.meta.players;
  for (const s of t.meta.car_touch_events) {
    const a = s.player_slot ?? (s.player_name ? n.findIndex((h) => h.name === s.player_name) : -1), c = s.victim_slot ?? (s.victim_name ? n.findIndex((h) => h.name === s.victim_name) : -1);
    if (a < 0 || c < 0 || a === c || a >= 16 || c >= 16 || (((o = n[a]) == null ? void 0 : o.team) ?? 0) === (((i = n[c]) == null ? void 0 : i.team) ?? 0)) continue;
    const l = Math.min(a, c), m = Math.max(a, c), f = e.get(s.frame);
    f ? f.some(([h, r]) => h === l && r === m) || f.push([l, m]) : e.set(s.frame, [[l, m]]);
  }
  return e;
}
function xe(t, e, n) {
  const o = t.getTime(e);
  for (let i = e; i < t.frameCount && !(i > e + 2 && t.getTime(i) - o > 0.08); i++)
    if ((t.getCarFlagsLo(i, n) & O.DEMOLISHED) !== 0) return !0;
  return !1;
}
function je(t, e, n, o, i) {
  if (i.length === 0) return !1;
  const s = t.getTime(e);
  let a = !1;
  for (let _ = 0; _ < i.length; _++)
    if (Math.abs(s - i[_]) <= 0.35) {
      a = !0;
      break;
    }
  if (!a) return !1;
  const c = t.ballKinBase(e), l = t.ballKin[c], m = t.ballKin[c + 1], f = t.ballKin[c + 2], h = t.carKinBase(e, n), r = t.carKinBase(e, o), u = t.carKin, p = Math.hypot(u[h] - l, u[h + 1] - m, u[h + 2] - f), g = Math.hypot(u[r] - l, u[r + 1] - m, u[r + 2] - f);
  return p <= 380 && g <= 380;
}
function et(t, e, n, o, i) {
  const s = t[e + 6], a = t[e + 7], c = t[e + 8], l = t[e + 9], m = 1 - 2 * (a * a + c * c), f = 2 * (s * a + l * c), h = 2 * (s * c - l * a);
  return m * n + f * o + h * i;
}
function tt(t, e, n, o, i, s) {
  const a = t.carKin, c = t.carKinBase(n, o), l = t.carKinBase(n, i), m = a[c + 3], f = a[c + 4], h = a[c + 5], r = a[l + 3], u = a[l + 4], p = a[l + 5];
  let g = e;
  const _ = t.carKinBase(e, o), b = t.carKinBase(e, i);
  let d = Math.hypot(a[_ + 3] - m, a[_ + 4] - f, a[_ + 5] - h), E = Math.hypot(a[b + 3] - r, a[b + 4] - u, a[b + 5] - p);
  if (s && e + 1 < t.frameCount) {
    const P = e + 1, ne = t.getTime(P) - t.getTime(e), x = t.getCarFlagsLo(P, o), I = t.getCarFlagsLo(P, i);
    if (ne > 0 && ne <= 0.06 && t.getGameCountdown(P) === 0 && (t.getGameFlags(P) & fe.MATCH_ENDED) === 0 && (x & O.ACTIVE) !== 0 && (x & O.DEMOLISHED) === 0 && (I & O.ACTIVE) !== 0 && (I & O.DEMOLISHED) === 0) {
      const G = t.carKinBase(P, o), w = t.carKinBase(P, i), R = Math.hypot(a[G + 3] - m, a[G + 4] - f, a[G + 5] - h), k = Math.hypot(a[w + 3] - r, a[w + 4] - u, a[w + 5] - p);
      Math.max(R, k) > Math.max(d, E) && (g = P, d = R, E = k);
    }
  }
  const v = t.carKinBase(g, o), S = t.carKinBase(g, i), A = Math.hypot(a[l] - a[c], a[l + 1] - a[c + 1], a[l + 2] - a[c + 2]), M = Math.hypot(a[b] - a[_], a[b + 1] - a[_ + 1], a[b + 2] - a[_ + 2]), C = A > 1 && A <= M ? c : _, D = A > 1 && A <= M ? l : b, K = Math.max(1, Math.hypot(a[D] - a[C], a[D + 1] - a[C + 1], a[D + 2] - a[C + 2])), y = (a[D] - a[C]) / K, U = (a[D + 1] - a[C + 1]) / K, N = (a[D + 2] - a[C + 2]) / K, H = (a[S + 3] - r) * y + (a[S + 4] - u) * U + (a[S + 5] - p) * N, F = -((a[v + 3] - m) * y + (a[v + 4] - f) * U + (a[v + 5] - h) * N), Y = et(a, C, y, U, N), Z = et(a, D, -y, -U, -N), V = m * y + f * U + h * N, L = -(r * y + u * U + p * N), W = E - d + 0.5 * (H - F) + 400 * (Y - Z) + 0.35 * (V - L);
  return { dvA: d, dvB: E, scoreA: W, driveSum: V + L };
}
function sn(t, e, n, o, i, s) {
  var f, h;
  const a = Math.min(n.carSlotCount, t.length, 16), c = n.meta.players, l = (s == null ? void 0 : s.kickoffHitTimes) ?? [];
  if (s != null && s.carTouchByFrame) {
    const r = s.carTouchByFrame.get(o);
    if (!r || r.length === 0) return;
    for (const [u, p] of r) {
      if (u >= a || p >= a) continue;
      const g = n.getCarFlagsLo(o, u), _ = n.getCarFlagsLo(o, p);
      if ((g & O.ACTIVE) === 0 || (g & O.DEMOLISHED) !== 0 || (_ & O.ACTIVE) === 0 || (_ & O.DEMOLISHED) !== 0) continue;
      const b = u * 16 + p;
      if (e[b] > 0 || !t[u].hasPrev || !t[p].hasPrev || xe(n, o, u) || xe(n, o, p) || je(n, o, u, p, l)) continue;
      const d = tt(n, o, i, u, p, !0);
      Math.max(d.dvA, d.dvB) < 250 || (d.scoreA > 1e-3 ? (t[u].bumpsInflicted++, t[p].bumpsTaken++) : d.scoreA < -1e-3 && (t[p].bumpsInflicted++, t[u].bumpsTaken++), e[b] = 0.5);
    }
    return;
  }
  const m = n.carKin;
  for (let r = 0; r < a; r++) {
    const u = n.getCarFlagsLo(o, r);
    if ((u & O.ACTIVE) === 0 || (u & O.DEMOLISHED) !== 0) continue;
    const p = ((f = c[r]) == null ? void 0 : f.team) ?? 0, g = n.carKinBase(o, r);
    for (let _ = r + 1; _ < a; _++) {
      if ((((h = c[_]) == null ? void 0 : h.team) ?? 0) === p) continue;
      const b = n.getCarFlagsLo(o, _);
      if ((b & O.ACTIVE) === 0 || (b & O.DEMOLISHED) !== 0) continue;
      const d = r * 16 + _;
      if (e[d] > 0 || !t[r].hasPrev || !t[_].hasPrev) continue;
      const E = n.carKinBase(o, _);
      if (Math.hypot(m[E] - m[g], m[E + 1] - m[g + 1], m[E + 2] - m[g + 2]) > 175 || xe(n, o, r) || xe(n, o, _) || je(n, o, r, _, l)) continue;
      const S = tt(n, o, i, r, _, !1);
      Math.max(S.dvA, S.dvB) < 300 || S.driveSum < 400 || (S.scoreA > 1e-3 ? (t[r].bumpsInflicted++, t[_].bumpsTaken++) : S.scoreA < -1e-3 && (t[_].bumpsInflicted++, t[r].bumpsTaken++), e[d] = 0.5);
    }
  }
}
const nt = 892.5, cn = 321.5;
function ln(t, e, n) {
  var N;
  if (t.frameCount <= 0) return 0.28;
  const o = Math.max(0, Math.min(t.frameCount - 1, e)), i = t.ballKinBase(o), s = t.ballKin, a = s[i], c = s[i + 1], l = s[i + 2], m = s[i + 3], f = s[i + 4], h = s[i + 5], r = n === 0 ? 5120 : -5120, u = -a, p = r - c, g = cn - l, _ = Math.max(1, Math.hypot(u, p, g)), b = Math.max(0, (m * u + f * p + h * g) / _), d = -nt - a, E = nt - a, v = Math.max(1, Math.hypot(d, p)), S = Math.max(1, Math.hypot(E, p)), A = Math.max(-1, Math.min(1, (d * E + p * p) / (v * S))), M = Math.acos(A);
  let C = 2e3;
  const D = t.carKin, K = t.meta.players;
  for (let H = 0; H < t.carSlotCount; H++) {
    if ((((N = K[H]) == null ? void 0 : N.team) ?? 0) === n) continue;
    const F = t.getCarFlagsLo(o, H);
    if ((F & O.ACTIVE) === 0 || (F & O.DEMOLISHED) !== 0) continue;
    const Y = t.carKinBase(o, H), Z = Math.min(
      Math.hypot(D[Y] - a, D[Y + 1] - c, D[Y + 2] - l),
      Math.hypot(D[Y], D[Y + 1] - r, D[Y + 2] - 100)
    );
    Z < C && (C = Z);
  }
  const y = -2.15 + 1.85 * M + 65e-5 * b - 42e-5 * _ + 35e-5 * C, U = 1 / (1 + Math.exp(-y));
  return Math.max(0.02, Math.min(0.95, U));
}
class rn {
  static compute(e, n, o, i, s = e.is_mvp, a, c) {
    var h, r;
    const l = e.shots > 0 ? e.goals / e.shots * 100 : 0, m = n > 0 ? (e.goals + e.assists) / n * 100 : 0;
    let f = 0;
    if (e.shots > 0)
      if (a && a.frameCount > 0) {
        const u = [], p = e.team === 0 ? 1 : -1, g = (h = a.meta.shot_events) != null && h.length ? a.meta.events.filter((E) => (E.event_type ?? "").toLowerCase() === "shot" && !a.meta.shot_events.some((v) => Math.abs(v.timestamp - E.timestamp) <= 0.25 && (v.player_slot === E.player_slot || v.player_name != null && v.player_name === E.player_name))) : [], _ = (r = a.meta.shot_events) != null && r.length ? [...a.meta.shot_events, ...g] : a.meta.events, b = /* @__PURE__ */ new Set();
        for (const E of _) {
          if ((E.event_type ?? "").toLowerCase() !== "shot" || !(E.player_slot === e.slot || E.player_slot == null && E.player_name === e.name)) continue;
          let v = E.frame;
          if (c && c.length > 0) {
            const S = E.timestamp ?? a.getTime(Math.max(0, Math.min(a.frameCount - 1, E.frame)));
            let A = null;
            for (const M of c)
              M.slot !== e.slot || b.has(M.frame) || M.time >= S - 3 && M.time <= S + 0.08 && M.vy * p > 50 && (!A || M.time > A.time) && (A = M);
            A && (b.add(A.frame), v = A.frame);
          }
          u.includes(v) || u.push(v);
        }
        if (u.length < e.shots && c) {
          const E = e.team === 0 ? 5120 : -5120, v = c.filter((S) => S.slot === e.slot && S.vy * p > 200 && !b.has(S.frame)).map((S) => {
            const A = Math.max(0.05, (E - S.y) / S.vy), M = Math.abs(S.x + S.vx * A), C = M <= 1250 && A <= 3.5 ? 1e4 - M * 2 : -M;
            return { frame: S.frame, rank: C + S.vy * p * 0.8 + S.y * p * 0.4 };
          }).sort((S, A) => A.rank - S.rank);
          for (const S of v) {
            if (u.length >= e.shots) break;
            u.every((A) => Math.abs(a.getTime(A) - a.getTime(S.frame)) > 0.5) && u.push(S.frame);
          }
        }
        const d = u.slice(0, e.shots);
        for (const E of d)
          f += ln(a, E, e.team);
        d.length < e.shots && (f += (e.shots - d.length) * 0.28);
      } else
        f = e.shots * 0.28;
    return {
      direct: {
        score: e.score,
        goals: e.goals,
        assists: e.assists,
        saves: e.saves,
        epic_saves: e.epic_saves ?? null,
        shots: e.shots,
        clears: e.clears ?? null,
        centers: e.centers ?? null,
        mvp: s
      },
      derived: {
        shooting_percentage: Number(l.toFixed(1)),
        goal_participation: Number(m.toFixed(1)),
        shots_against: i,
        goals_against: o,
        xg: Number(f.toFixed(2))
      }
    };
  }
}
function ot(t) {
  const e = [];
  for (let n = 1; n < t.frameCount; n++)
    t.getGameCountdown(n - 1) > 0 && t.getGameCountdown(n) === 0 && e.push(n);
  if (e.length === 0 && t.frameCount > 1) {
    const n = t.ballKinBase(0);
    t.getTime(0) < 2 && Math.abs(t.ballKin[n]) < 60 && Math.abs(t.ballKin[n + 1]) < 60 && Math.hypot(t.ballKin[n + 3], t.ballKin[n + 4]) < 50 && e.push(0);
    for (let o = 1; o < t.frameCount; o++) {
      const i = t.ballKinBase(o - 1), s = t.ballKinBase(o);
      Math.hypot(t.ballKin[i], t.ballKin[i + 1]) > 1500 && Math.abs(t.ballKin[s]) < 60 && Math.abs(t.ballKin[s + 1]) < 60 && Math.hypot(t.ballKin[s + 3], t.ballKin[s + 4]) < 50 && e.push(o);
    }
  }
  return e;
}
function rt(t, e) {
  var s;
  const n = t.frameCount, o = t.getTime(e), i = t.meta.players;
  for (let a = e; a < n && !(a > e && t.getGameCountdown(a) > 0 || t.getTime(a) - o > 10); a++) {
    const c = t.ballKinBase(a), l = Math.hypot(t.ballKin[c + 3], t.ballKin[c + 4], t.ballKin[c + 5]), m = Math.hypot(t.ballKin[c], t.ballKin[c + 1]);
    if ((t.getGameFlags(a) & fe.BALL_HIT) !== 0 || l > 50 || m > 15) {
      const f = t.getTime(a);
      if (t.meta.first_touch_events) {
        const S = t.meta.first_touch_events.find(
          (A) => A.frame >= e && A.frame <= a + 4 || A.timestamp >= o && A.timestamp <= f + 0.18
        );
        if (S) {
          const A = S.player_slot ?? (S.player_name ? i.findIndex((M) => M.name === S.player_name) : -1);
          if (A >= 0 && A < t.carSlotCount)
            return { hitFrame: a, winnerSlots: [A] };
        }
      }
      const h = [], r = Math.max(e, a - 1), u = t.ballKinBase(r);
      for (let S = 0; S < t.carSlotCount; S++) {
        const A = t.getCarFlagsLo(a, S);
        if ((A & O.ACTIVE) === 0 || (A & O.DEMOLISHED) !== 0) continue;
        const M = t.carKinBase(a, S), C = t.carKin[M], D = t.carKin[M + 1], K = t.carKin[M + 2], y = Math.hypot(C - t.ballKin[c], D - t.ballKin[c + 1], K - t.ballKin[c + 2]), U = Math.hypot(C, D, K - 93), N = t.carKinBase(r, S), H = Math.hypot(t.carKin[N] - t.ballKin[u], t.carKin[N + 1] - t.ballKin[u + 1], t.carKin[N + 2] - t.ballKin[u + 2]);
        if (Math.min(y, H, U) > 290) continue;
        const F = Math.min(y, U) + 0.5 * H;
        h.push({ slot: S, team: (((s = i[S]) == null ? void 0 : s.team) ?? 0) === 1 ? 1 : 0, score: F });
      }
      let p = 1 / 0;
      for (const S of h) S.score < p && (p = S.score);
      const g = h.filter((S) => S.score <= p + 45), _ = t.getLastTouchSlot(a), b = _ < t.carSlotCount && i[_] ? i[_].team === 1 ? 1 : 0 : null, d = b !== null && g.some((S) => S.team === b) ? g.filter((S) => S.team === b) : h;
      let E = 1 / 0;
      for (const S of d) S.score < E && (E = S.score);
      let v = d.filter((S) => S.score <= E + 1e-3);
      if (v.some((S) => S.team === 0) && v.some((S) => S.team === 1)) {
        const S = t.ballKin[c + 4];
        S > 50 ? v = v.filter((A) => A.team === 0) : S < -50 && (v = v.filter((A) => A.team === 1));
      }
      return { hitFrame: a, winnerSlots: v.map((S) => S.slot) };
    }
  }
  return { hitFrame: -1, winnerSlots: [] };
}
function mn(t, e) {
  const n = [], o = [];
  for (const i of e) {
    n.push(t.getTime(i));
    const { hitFrame: s } = rt(t, i);
    s >= 0 && o.push(t.getTime(s));
  }
  return { goTimes: n, hitTimes: o };
}
function fn(t, e, n, o, i) {
  const s = t.frameCount, a = Math.max(0, Math.min(s - 1, e.frame)), c = t.getTime(a), l = n === 0 ? 1 : -1, m = e.player_slot ?? (e.player_name ? t.meta.players.findIndex((p) => p.name === e.player_name && p.team === n) : -1);
  let f = null, h = null;
  for (const p of o)
    p.team !== n || p.frame > a || p.time < c - 3.5 || p.time > c || p.vy * l <= 50 || (m >= 0 && p.slot === m && (!f || p.time > f.time) && (f = p), (!h || p.time > h.time) && (h = p));
  if (f) return f.frame;
  if (h) return h.frame;
  const r = c - 0.7;
  let u = a;
  for (let p = a; p >= 0 && !(p < a && i[p] === 0 || (u = p, t.getTime(p) <= r + 1e-4)); p--)
    ;
  return u;
}
const it = 0.17;
function un(t, e) {
  const n = t.getTime(e), o = t.ballKin;
  let i = e - 1;
  for (; i > 0 && n - t.getTime(i - 1) <= 0.036 && !(t.getGameCountdown(i - 1) > 0 || (t.getGameFlags(i - 1) & fe.MATCH_ENDED) !== 0); ) {
    const s = t.ballKinBase(i - 1), a = t.ballKinBase(i);
    if ((o[a] - o[s]) ** 2 + (o[a + 1] - o[s + 1]) ** 2 + (o[a + 2] - o[s + 2]) ** 2 > 1500 * 1500) break;
    i--;
  }
  return i;
}
function mt(t, e) {
  var h, r;
  const n = t.frameCount, o = t.ballKin, i = t.ballKinBase(e), s = o[i + 1], a = t.getTime(e), c = t.getGameBlueScore(e), l = t.getGameOrangeScore(e);
  for (const u of t.meta.events)
    if ((u.event_type ?? "").toLowerCase() === "goal" && u.frame >= e && u.timestamp >= a && u.timestamp <= a + it + 0.05) {
      const p = u.team ?? (u.player_slot != null ? (h = t.meta.players[u.player_slot]) == null ? void 0 : h.team : (r = t.meta.players.find((g) => g.name === u.player_name)) == null ? void 0 : r.team);
      if (p === 0) return 1e4;
      if (p === 1) return -1e4;
    }
  let m = e;
  for (; m + 1 < n; ) {
    const u = m + 1;
    if (t.getGameBlueScore(u) > c) return 1e4;
    if (t.getGameOrangeScore(u) > l) return -1e4;
    if (t.getTime(u) - a > it) break;
    const p = t.ballKinBase(m);
    if (t.getGameCountdown(u) > 0 || (t.getGameFlags(u) & fe.MATCH_ENDED) !== 0) {
      if (o[p + 1] > 5e3 && o[p + 4] > 0) return 1e4;
      if (o[p + 1] < -5e3 && o[p + 4] < 0) return -1e4;
      break;
    }
    const g = t.ballKinBase(u);
    if ((o[g] - o[p]) ** 2 + (o[g + 1] - o[p + 1]) ** 2 + (o[g + 2] - o[p + 2]) ** 2 > 1500 * 1500) {
      if (o[p + 1] > 4800 && o[p + 4] > 0) return 1e4;
      if (o[p + 1] < -4800 && o[p + 4] < 0) return -1e4;
      break;
    }
    m = u;
  }
  const f = t.ballKinBase(m);
  return o[f + 4] + (o[f + 1] - s) * 2;
}
function _n(t, e, n, o, i, s) {
  var K;
  for (let y = 0; y < e.length; y++)
    e[y] > 0 && (e[y] = Math.max(0, e[y] - i));
  if (o <= 0) return;
  const a = Math.min(n.carSlotCount, t.length, 16), c = n.meta.players, l = n.carKin, m = n.ballKin, f = n.ballKinBase(o - 1), h = n.ballKinBase(o), r = m[h], u = m[h + 1], p = m[h + 2], g = i < 0.025 ? un(n, o) : o - 1;
  sn(t, e, n, o, g, s);
  let _ = -1, b = 260 + 1e-3, d = -1 / 0, E = !1, v = -1, S = 260 + 1e-3, A = -1 / 0, M = !1;
  for (let y = 0; y < a; y++) {
    const U = n.getCarFlagsLo(o, y);
    if ((U & O.ACTIVE) === 0 || (U & O.DEMOLISHED) !== 0) continue;
    const N = ((K = c[y]) == null ? void 0 : K.team) ?? 0, H = n.carKinBase(o, y), F = r - l[H], Y = u - l[H + 1], Z = p - l[H + 2], V = Math.hypot(F, Y, Z), L = (l[H + 3] * F + l[H + 4] * Y + l[H + 5] * Z) / Math.max(1, V);
    N === 1 ? V < S - 1e-3 ? (S = V, v = y, A = L, M = !1) : Math.abs(V - S) <= 1e-3 && (v < 0 || L > A + 1e-3 ? (S = V, v = y, A = L, M = !1) : Math.abs(L - A) <= 1e-3 && (M = !0)) : V < b - 1e-3 ? (b = V, _ = y, d = L, E = !1) : Math.abs(V - b) <= 1e-3 && (_ < 0 || L > d + 1e-3 ? (b = V, _ = y, d = L, E = !1) : Math.abs(L - d) <= 1e-3 && (E = !0));
  }
  const C = (r - m[f]) ** 2 + (u - m[f + 1]) ** 2 + (p - m[f + 2]) ** 2;
  let D = Math.hypot(m[h + 3] - m[f + 3], m[h + 4] - m[f + 4], m[h + 5] - (m[f + 5] - 650 * i));
  if (D < 260 && g < o - 1) {
    const y = n.ballKinBase(g), U = Math.max(i, n.getTime(o) - n.getTime(g)), N = Math.hypot(m[h + 3] - m[y + 3], m[h + 4] - m[y + 4], m[h + 5] - (m[y + 5] - 650 * U));
    N > D && (D = N);
  }
  if (_ >= 0 && v >= 0 && !E && !M && D >= 260 && C <= 2500 * 2500 && e[256] <= 0) {
    const y = t[_], U = t[v];
    y.challengesContested++, U.challengesContested++, y.challengeLatencySumMs += Math.min(800, Math.max(40, (y.challengeApproachTimeSec + i) * 1e3)), U.challengeLatencySumMs += Math.min(800, Math.max(40, (U.challengeApproachTimeSec + i) * 1e3)), y.challengeApproachTimeSec = 0, U.challengeApproachTimeSec = 0;
    const N = mt(n, o);
    N > 60 ? (y.challengesWon++, U.challengesLost++) : N < -60 && (U.challengesWon++, y.challengesLost++), e[256] = 0.6;
  }
}
function hn(t, e, n, o, i, s, a) {
  const c = 2 * (-i * n + s * e), l = 2 * (-s * t + o * n), m = 2 * (-o * e + i * t);
  return [
    t + a * c + (-i * m + s * l),
    e + a * l + (-s * c + o * m),
    n + a * m + (-o * l + i * c)
  ];
}
class he {
  static createInitialState() {
    return {
      bumpsInflicted: 0,
      bumpsTaken: 0,
      bumpCooldownSec: 0,
      challengesContested: 0,
      challengesWon: 0,
      challengesLost: 0,
      challengeCooldownSec: 0,
      challengeLatencySumMs: 0,
      challengeApproachTimeSec: 0,
      flipResetCount: 0,
      flipResetDelaySum: 0,
      airBallContactTime: 0,
      wasGrounded: !0,
      tookOffFromGround: !1,
      takeoffTime: 0,
      usedFlipInAir: !1,
      prevDodged: !1,
      prevDoubleJumped: !1,
      prevVx: 0,
      prevVy: 0,
      prevVz: 0,
      prevBallVx: 0,
      prevBallVy: 0,
      prevBallVz: 0,
      hasPrev: !1
    };
  }
  static resetContinuity(e) {
    e.hasPrev = !1, e.airBallContactTime = 0, e.wasGrounded = !0, e.tookOffFromGround = !1, e.usedFlipInAir = !1, e.prevDodged = !1, e.prevDoubleJumped = !1, e.challengeApproachTimeSec = 0;
  }
  static evaluateFramePairs(e, n, o, i, s, a) {
    _n(e, n, o, i, s, a);
  }
  static processFrame(e, n, o, i, s, a = !1) {
    var Y, Z;
    const c = n.meta.players, l = ((Y = c[i]) == null ? void 0 : Y.team) ?? 0, m = n.carKin, f = n.carKinBase(o, i), h = m[f], r = m[f + 1], u = m[f + 2], p = m[f + 3], g = m[f + 4], _ = m[f + 5], b = m[f + 6], d = m[f + 7], E = m[f + 8], v = m[f + 9], S = n.ballKinBase(o), A = n.ballKin[S], M = n.ballKin[S + 1], C = n.ballKin[S + 2], D = Math.hypot(h - A, r - M, u - C);
    let K = 1 / 0;
    for (let V = 0; V < n.carSlotCount; V++) {
      if (V === i || (((Z = c[V]) == null ? void 0 : Z.team) ?? 0) === l) continue;
      const L = n.getCarFlagsLo(o, V);
      if ((L & O.ACTIVE) === 0 || (L & O.DEMOLISHED) !== 0) continue;
      const W = n.carKinBase(o, V), P = Math.hypot(m[W] - A, m[W + 1] - M, m[W + 2] - C);
      P < K && (K = P);
    }
    if (D <= 950 && K <= 950 ? e.challengeApproachTimeSec += s : e.challengeApproachTimeSec = 0, !a && e.hasPrev && o > 0) {
      e.challengeCooldownSec > 0 && (e.challengeCooldownSec = Math.max(0, e.challengeCooldownSec - s));
      const V = n.ballKin[S + 3], L = n.ballKin[S + 4], W = n.ballKin[S + 5], P = Math.hypot(V - e.prevBallVx, L - e.prevBallVy, W - (e.prevBallVz - 650 * s));
      if (D <= 260 && K <= 260 && P >= 260 && e.challengeCooldownSec <= 0) {
        e.challengesContested++;
        const ne = mt(n, o) * (l === 1 ? -1 : 1);
        ne > 60 ? e.challengesWon++ : ne < -60 && e.challengesLost++, e.challengeLatencySumMs += Math.min(800, Math.max(40, e.challengeApproachTimeSec * 1e3)), e.challengeCooldownSec = 0.6;
      }
      e.prevBallVx = V, e.prevBallVy = L, e.prevBallVz = W;
    }
    e.prevVx = p, e.prevVy = g, e.prevVz = _, e.hasPrev = !0;
    const y = n.getCarFlagsLo(o, i), U = (y & O.ON_GROUND) !== 0 || u <= 25, N = (y & O.DODGED) !== 0, H = (y & O.DOUBLE_JUMPED) !== 0, F = n.getTime(o);
    if (U)
      e.airBallContactTime = 0, e.wasGrounded = !0, e.tookOffFromGround = !1, e.usedFlipInAir = !1;
    else {
      e.wasGrounded && (e.wasGrounded = !1, e.tookOffFromGround = o > 0, e.takeoffTime = F, e.usedFlipInAir = !1);
      const V = N && !e.prevDodged, L = H && !e.prevDoubleJumped, W = !e.tookOffFromGround || e.usedFlipInAir || F - e.takeoffTime >= 1.4;
      if (e.airBallContactTime > 0 && V && F > e.airBallContactTime && W)
        e.flipResetCount++, e.flipResetDelaySum += F - e.airBallContactTime, e.airBallContactTime = 0;
      else if (u > 160 && D <= 185 && !N && e.airBallContactTime === 0) {
        const [P, ne, x] = hn(A - h, M - r, C - u, b, d, E, v);
        x < -15 && x >= -135 && Math.abs(P - 10) <= 95 && Math.abs(ne) <= 75 && (e.airBallContactTime = F);
      }
      (V || L) && (e.usedFlipInAir = !0);
    }
    e.prevDodged = N, e.prevDoubleJumped = H;
  }
  static finalizePhysicality(e, n, o, i, s, a = !1) {
    let c = 0, l = 0;
    for (const m of o)
      m.event_type !== "demo" && m.event_type !== "demolition" || a && s && m.frame >= 0 && m.frame < s.length && s[m.frame] === 0 || ((m.player_slot === e || m.player_slot == null && m.player_name === n) && c++, (m.victim_slot === e || m.victim_slot == null && m.victim_name === n) && l++);
    return { inflicted: c, taken: l, bumps_inflicted: i.bumpsInflicted, bumps_taken: i.bumpsTaken };
  }
  static finalizeStarvation(e) {
    const n = e.countCollectedBig > 0 ? e.countStolenBig / e.countCollectedBig * 100 : 0, o = e.totalBoostConsumed > 0 ? e.kineticEnergyGainKuu / e.totalBoostConsumed : 0, i = e.countCollectedSmall > 0 ? e.smoothSmallPadCount / e.countCollectedSmall * 100 : 0;
    return {
      starvation_pressure_index: Number(n.toFixed(1)),
      kinetic_energy_conversion: Number(o.toFixed(2)),
      pad_pathing_efficiency: Number(i.toFixed(1)),
      boost_starvation_time_inflicted: Number(e.starvationTimeInflicted.toFixed(1))
    };
  }
  static finalizeMicroMechanics(e, n, o) {
    const i = e.landingCount > 0 ? e.landingScoreSum / e.landingCount : 100, s = e.totalClimbBoost > 0 ? e.totalClimbUU / e.totalClimbBoost : 0, a = (o == null ? void 0 : o.flip_resets) != null ? o.flip_resets : n.flipResetCount, c = n.flipResetCount > 0 && a > 0 ? n.flipResetDelaySum / n.flipResetCount : 0;
    return {
      flip_reset_count: a,
      flip_reset_delay_avg: Number(c.toFixed(2)),
      landing_recovery_quality: Number(i.toFixed(1)),
      aerial_energy_efficiency: Number(s.toFixed(1)),
      wavedash_count: e.wavedashCount,
      aerial_hits: (o == null ? void 0 : o.aerial_hits) ?? null,
      crossbar_hits: (o == null ? void 0 : o.crossbar_hits) ?? null
    };
  }
  static finalizeChallenges(e) {
    const n = e.challengesContested, o = e.challengesWon, i = e.challengesLost, s = o + i, a = s > 0 ? o / s * 100 : n > 0 ? 50 : 0, c = n > 0 ? e.challengeLatencySumMs / n : 0;
    return {
      challenges_contested: n,
      challenges_won: o,
      challenges_lost: i,
      challenge_win_rate: Number(a.toFixed(1)),
      time_to_challenge_latency: Number(c.toFixed(1))
    };
  }
}
const ft = 4096, ye = 5120, Ue = 2048, De = 256, Ie = 190, dn = 8064, ve = 892.5, pe = 643, at = 6e3, re = 250;
function ce(t, e, n, o, i, s, a, c) {
  const l = Math.hypot(i, s, a) || 1, m = i / l, f = s / l, h = a / l;
  return {
    distance: o,
    position: [t - o * m, e - o * f, n - o * h],
    normal: [m, f, h],
    surface: c
  };
}
function pn(t, e) {
  const n = t < 0 ? -1 : 1, o = e < 0 ? -1 : 1, i = Math.abs(t), s = Math.abs(e), a = ft - i, c = ye - s, l = (dn - (i + s)) * Math.SQRT1_2;
  return l <= a && l <= c ? {
    d: l,
    nx: -n * Math.SQRT1_2,
    ny: -o * Math.SQRT1_2,
    surface: "corner"
  } : a <= c ? { d: a, nx: -n, ny: 0, surface: "sidewall" } : { d: c, nx: 0, ny: -o, surface: "backboard" };
}
function gn(t, e, n) {
  const o = t < 0 ? -1 : 1, i = e < 0 ? -1 : 1, s = Math.abs(t), a = Math.abs(e);
  if (s > ve || n > pe) {
    const _ = ye - a, b = ve - s, d = pe - n;
    return _ >= b && _ >= d ? ce(t, e, n, _, 0, -i, 0, "backboard") : s > ve && (n <= pe || b >= d) ? ce(t, e, n, b, -o, 0, 0, "goal_post") : ce(t, e, n, d, 0, 0, -1, "goal_crossbar");
  }
  const c = s - (ve - re), l = a - (at - re);
  let m, f, h, r;
  if (c > 0 && l > 0) {
    const _ = Math.hypot(c, l) || 1;
    m = re - _, f = -o * c / _, h = -i * l / _, r = "goal_ramp";
  } else c > l ? (m = ve - s, f = -o, h = 0, r = "goal_wall") : (m = at - a, f = 0, h = -i, r = "goal_back");
  const u = a > ye + re;
  if (u && n < re && m < re) {
    const _ = re - m, b = re - n;
    return ce(t, e, n, re - Math.hypot(_, b), _ * f, _ * h, b, "goal_ramp");
  }
  if (u && n > pe - re && m < re) {
    const _ = re - m, b = n - (pe - re);
    return ce(t, e, n, re - Math.hypot(_, b), _ * f, _ * h, -b, "goal_ramp");
  }
  const p = n, g = pe - n;
  return p <= g && p <= m ? ce(t, e, n, p, 0, 0, 1, "floor") : g <= m ? ce(t, e, n, g, 0, 0, -1, "goal_ceiling") : ce(t, e, n, m, f, h, 0, r);
}
function vn(t) {
  const e = t[0], n = t[1], o = t[2], i = Math.abs(e), s = Math.abs(n), a = e < 0 ? -1 : 1, c = n < 0 ? -1 : 1;
  if (s >= ye)
    return gn(e, n, o);
  const l = pn(e, n), m = i < ve && o < pe;
  if (!m && o < De && l.d < De) {
    const r = De - l.d, u = De - o;
    return ce(e, n, o, De - Math.hypot(r, u), r * l.nx, r * l.ny, u, "floor_ramp");
  }
  if (o > Ue - Ie && l.d < Ie) {
    const r = Ie - l.d, u = o - (Ue - Ie);
    return ce(e, n, o, Ie - Math.hypot(r, u), r * l.nx, r * l.ny, -u, "ceiling_ramp");
  }
  const f = o, h = Ue - o;
  if (m && l.surface === "backboard") {
    const r = ye - s, u = Math.hypot(ve - i, r), p = Math.hypot(r, pe - o), g = ft - i;
    let _ = f, b = ce(e, n, o, f, 0, 0, 1, "floor");
    return h < _ && (_ = h, b = ce(e, n, o, h, 0, 0, -1, "ceiling")), g < _ && (_ = g, b = ce(e, n, o, g, -a, 0, 0, "sidewall")), u < _ && (_ = u, b = ce(e, n, o, u, a * (i - ve), c * (s - ye), 0, "goal_post")), p < _ && (b = ce(e, n, o, p, 0, c * (s - ye), o - pe, "goal_crossbar")), b;
  }
  return f <= h && f <= l.d ? ce(e, n, o, f, 0, 0, 1, "floor") : h <= l.d ? ce(e, n, o, h, 0, 0, -1, "ceiling") : ce(e, n, o, l.d, l.nx, l.ny, 0, l.surface);
}
class Te {
  static createInitialState() {
    return {
      speedTimeIntegral: 0,
      activeTime: 0,
      totalDistance: 0,
      timeSupersonic: 0,
      timeBoostSpeed: 0,
      timeSlowSpeed: 0,
      timeGround: 0,
      timeLowAir: 0,
      timeHighAir: 0,
      timePowerslide: 0,
      countPowerslide: 0,
      prevPowerslide: !1,
      hasPrevPos: !1,
      prevX: 0,
      prevY: 0,
      prevZ: 0,
      prevGrounded: !0,
      prevDodged: !1,
      isSupersonicActive: !1,
      supersonicGraceSec: 0,
      landingScoreSum: 0,
      landingCount: 0,
      wavedashCount: 0,
      consecutiveAirTime: 0,
      dodgeStartAgeSec: 99,
      airTakeoffZ: 17,
      airApexZ: 17,
      airBoostUsed: 0,
      totalClimbUU: 0,
      totalClimbBoost: 0
    };
  }
  static resetContinuity(e) {
    e.hasPrevPos = !1, e.prevPowerslide = !1, e.prevGrounded = !0, e.prevDodged = !1, e.isSupersonicActive = !1, e.supersonicGraceSec = 0, e.consecutiveAirTime = 0, e.dodgeStartAgeSec = 99, e.airBoostUsed = 0;
  }
  static processSample(e, n, o, i, s, a, c, l, m, f, h, r, u, p, g, _ = !1) {
    const b = Math.hypot(s, a, c);
    if (e.hasPrevPos) {
      const K = Math.hypot(n - e.prevX, o - e.prevY, i - e.prevZ);
      K < 1500 ? e.totalDistance += K : (e.prevPowerslide = !1, e.prevDodged = !1, e.isSupersonicActive = !1, e.supersonicGraceSec = 0, e.consecutiveAirTime = 0, e.dodgeStartAgeSec = 99, e.airBoostUsed = 0);
    }
    e.hasPrevPos = !0, e.prevX = n, e.prevY = o, e.prevZ = i, e.speedTimeIntegral += b * r, e.activeTime += r, b >= Yt || p ? (e.isSupersonicActive = !0, e.supersonicGraceSec = Wt) : e.isSupersonicActive && b >= $t && e.supersonicGraceSec > 1e-6 ? e.supersonicGraceSec = Math.max(0, e.supersonicGraceSec - r) : (e.isSupersonicActive = !1, e.supersonicGraceSec = 0);
    const d = e.isSupersonicActive;
    d ? e.timeSupersonic += r : b > Ht ? e.timeBoostSpeed += r : e.timeSlowSpeed += r;
    const E = 2 * (l * f + h * m), v = 2 * (m * f - h * l), S = 1 - 2 * (l * l + m * m), A = vn([n, o, i]), M = E * A.normal[0] + v * A.normal[1] + S * A.normal[2], C = i <= 25 && S > 0.3 || u && M > 0.45 || A.distance <= 26 && M >= 0.65;
    if (_ && !e.prevDodged ? e.dodgeStartAgeSec = 0 : e.dodgeStartAgeSec += r, C) {
      if (e.timeGround += r, !e.prevGrounded) {
        e.consecutiveAirTime > 0.25 && (e.landingCount++, e.landingScoreSum += Math.max(0, Math.min(1, M)) * 100);
        const K = e.airApexZ - e.airTakeoffZ;
        K >= 120 && e.airBoostUsed >= 3 && (e.totalClimbUU += K, e.totalClimbBoost += e.airBoostUsed), e.dodgeStartAgeSec <= 0.22 && e.consecutiveAirTime >= 0.1 && e.consecutiveAirTime <= 1.25 && b > 450 && M >= 0.65 && e.wavedashCount++;
      }
      e.consecutiveAirTime = 0, e.dodgeStartAgeSec = 99, e.airBoostUsed = 0;
    } else
      e.prevGrounded ? (e.airTakeoffZ = i, e.airApexZ = i, e.airBoostUsed = 0) : i > e.airApexZ && (e.airApexZ = i), e.consecutiveAirTime += r, i <= Vt ? e.timeLowAir += r : e.timeHighAir += r;
    e.prevGrounded = C, e.prevDodged = _;
    const D = g && C;
    return D && (e.timePowerslide += r, e.prevPowerslide || e.countPowerslide++), e.prevPowerslide = D, { speed: b, isSupersonic: d, isEffectivelyGrounded: C };
  }
  static recordAirBoost(e, n, o) {
    !o && n > 0 && (e.airBoostUsed += n);
  }
  static finalize(e, n) {
    const o = e.activeTime > 1e-4 ? e.activeTime : Math.max(1e-3, n), i = e.activeTime > 1e-4, s = i ? e.speedTimeIntegral / e.activeTime : 0, a = s / Gt * 100;
    return {
      avg_speed: Number(s.toFixed(1)),
      avg_speed_percentage: Number(a.toFixed(1)),
      total_distance: Math.round(e.totalDistance),
      time_supersonic_speed: Number(e.timeSupersonic.toFixed(1)),
      percent_supersonic: i ? Number((e.timeSupersonic / o * 100).toFixed(1)) : 0,
      time_boost_speed: Number(e.timeBoostSpeed.toFixed(1)),
      percent_boost_speed: i ? Number((e.timeBoostSpeed / o * 100).toFixed(1)) : 0,
      time_slow_speed: Number(e.timeSlowSpeed.toFixed(1)),
      percent_slow_speed: i ? Number((e.timeSlowSpeed / o * 100).toFixed(1)) : 0,
      time_ground: Number(e.timeGround.toFixed(1)),
      time_low_air: Number(e.timeLowAir.toFixed(1)),
      time_high_air: Number(e.timeHighAir.toFixed(1)),
      percent_ground: i ? Number((e.timeGround / o * 100).toFixed(1)) : 0,
      percent_low_air: i ? Number((e.timeLowAir / o * 100).toFixed(1)) : 0,
      percent_high_air: i ? Number((e.timeHighAir / o * 100).toFixed(1)) : 0,
      time_powerslide: Number(e.timePowerslide.toFixed(1)),
      count_powerslide: e.countPowerslide,
      avg_powerslide_duration: e.countPowerslide > 0 ? Number((e.timePowerslide / e.countPowerslide).toFixed(2)) : 0
    };
  }
}
const Fe = [-3300, -2200, -1100, 0, 1100, 2200, 3300], Le = [-4200, -2520, -840, 840, 2520, 4200], $e = Fe.length * Le.length;
function Sn(t, e, n, o, i, s) {
  const a = Math.hypot(t - n, e - o), c = Math.hypot(n - i, o - s), l = Math.hypot(i - t, s - e);
  if (c <= 100 || l <= 100 || a <= 100) return 1;
  const m = 180 / Math.PI, f = Math.acos(Math.max(-1, Math.min(1, (l * l + a * a - c * c) / (2 * l * a)))) * m, h = Math.acos(Math.max(-1, Math.min(1, (c * c + a * a - l * l) / (2 * c * a)))) * m, r = Math.acos(Math.max(-1, Math.min(1, (c * c + l * l - a * a) / (2 * c * l)))) * m;
  return Math.min(1, ((f - 60) ** 2 + (h - 60) ** 2 + (r - 60) ** 2) / 21600);
}
function bn(t, e, n, o = 1) {
  const i = Math.min(e.carSlotCount, t.length);
  if (i <= 0) return 0;
  const s = e.carKin, a = [], c = [], l = [];
  for (let f = 0; f < i; f++) {
    const h = e.getCarFlagsLo(n, f);
    if ((h & O.ACTIVE) === 0 || (h & O.DEMOLISHED) !== 0) continue;
    t[f].voronoiTotalEvaluations += $e * o, a.push(f);
    const r = e.carKinBase(n, f);
    c.push(s[r]), l.push(s[r + 1]);
  }
  const m = a.length;
  if (m === 0) return 0;
  for (let f = 0; f < Fe.length; f++) {
    const h = Fe[f];
    for (let r = 0; r < Le.length; r++) {
      const u = Le[r];
      let p = 1 / 0;
      for (let d = 0; d < m; d++) {
        const E = (c[d] - h) ** 2 + (l[d] - u) ** 2;
        E < p && (p = E);
      }
      let g = 0;
      const _ = p * 1e-6 + 1e-3;
      for (let d = 0; d < m; d++)
        (c[d] - h) ** 2 + (l[d] - u) ** 2 <= p + _ && g++;
      const b = o / Math.max(1, g);
      for (let d = 0; d < m; d++)
        (c[d] - h) ** 2 + (l[d] - u) ** 2 <= p + _ && (t[a[d]].voronoiPointsOwned += b);
    }
  }
  return $e * o;
}
function Tn(t, e, n, o, i = 1) {
  const s = e.getCarFlagsLo(n, o);
  if ((s & O.ACTIVE) === 0 || (s & O.DEMOLISHED) !== 0) return;
  const a = e.carKin, c = [], l = [], m = [];
  for (let h = 0; h < e.carSlotCount; h++) {
    const r = e.getCarFlagsLo(n, h);
    if ((r & O.ACTIVE) === 0 || (r & O.DEMOLISHED) !== 0) continue;
    c.push(h);
    const u = e.carKinBase(n, h);
    l.push(a[u]), m.push(a[u + 1]);
  }
  t.voronoiTotalEvaluations += $e * i;
  const f = c.length;
  for (let h = 0; h < Fe.length; h++) {
    const r = Fe[h];
    for (let u = 0; u < Le.length; u++) {
      const p = Le[u];
      let g = 1 / 0;
      for (let E = 0; E < f; E++) {
        const v = (l[E] - r) ** 2 + (m[E] - p) ** 2;
        v < g && (g = v);
      }
      let _ = 0, b = !1;
      const d = g * 1e-6 + 1e-3;
      for (let E = 0; E < f; E++)
        (l[E] - r) ** 2 + (m[E] - p) ** 2 <= g + d && (_++, c[E] === o && (b = !0));
      b && (t.voronoiPointsOwned += i / Math.max(1, _));
    }
  }
}
function yn(t) {
  if (t.length === 0) return null;
  const e = t.map((i) => Math.floor(i.rawTenths + 1e-9));
  let n = 1e3 - e.reduce((i, s) => i + s, 0);
  if (n < 0 || n > t.length) return null;
  if (n > 0) {
    const i = t.map((a, c) => ({ idx: c, rem: a.rawTenths - e[c] })).sort((a, c) => c.rem - a.rem);
    let s = 0;
    for (; s < i.length && n > 0; ) {
      let a = s + 1;
      for (; a < i.length && Math.abs(i[a].rem - i[s].rem) <= 1e-6; ) a++;
      const c = a - s;
      if (c <= n) {
        for (let l = s; l < a; l++) e[i[l].idx] += 1;
        n -= c;
      }
      s = a;
    }
    if (n !== 0) return null;
  }
  const o = {};
  for (let i = 0; i < t.length; i++) o[t[i].key] = e[i] / 10;
  return o;
}
class de {
  static createInitialState() {
    return {
      activeTime: 0,
      timeDefThird: 0,
      timeNeutralThird: 0,
      timeOffThird: 0,
      timeDefHalf: 0,
      timeOffHalf: 0,
      timeBehindBall: 0,
      timeInFrontBall: 0,
      timeMostBack: 0,
      timeMostForward: 0,
      timeClosestToBall: 0,
      timeFarthestFromBall: 0,
      distBallIntegral: 0,
      distBallPossIntegral: 0,
      timePossession: 0,
      distBallNoPossIntegral: 0,
      timeNoPossession: 0,
      distMatesIntegral: 0,
      timeWithMates: 0,
      hasPrevActive: !1,
      prevInDeepDefBox: !1,
      totalDefEntries: 0,
      backpostEntries: 0,
      doubleCommits: 0,
      doubleCommitCooldownSec: 0,
      doubleCommitSustainedSec: 0,
      doubleCommitTimes: [],
      thirdManOverextensionIntegral: 0,
      thirdManTime: 0,
      spacingEntropyIntegral: 0,
      spacingEntropyTime: 0,
      shadowDefenseCompliantTime: 0,
      shadowDefenseTotalTime: 0,
      voronoiPointsOwned: 0,
      voronoiTotalEvaluations: 0,
      boxEntryConcessions: 0,
      prevBallInBox: !1,
      goalsAgainstAsLastMan: 0
    };
  }
  static resetContinuity(e) {
    e.hasPrevActive = !1, e.prevInDeepDefBox = !1, e.prevBallInBox = !1, e.doubleCommitSustainedSec = 0;
  }
  static isMostBackAtFrame(e, n, o) {
    var f, h;
    const i = e.getCarFlagsLo(n, o);
    if ((i & O.ACTIVE) === 0 || (i & O.DEMOLISHED) !== 0) return !1;
    const s = e.meta.players, a = ((f = s[o]) == null ? void 0 : f.team) ?? 0, c = a === 1 ? -1 : 1, l = e.carKin[e.carKinBase(n, o) + 1] * c;
    let m = 0;
    for (let r = 0; r < e.carSlotCount; r++) {
      if (r === o || ((h = s[r]) == null ? void 0 : h.team) !== a) continue;
      const u = e.getCarFlagsLo(n, r);
      if ((u & O.ACTIVE) === 0 || (u & O.DEMOLISHED) !== 0) continue;
      m++;
      const p = e.carKin[e.carKinBase(n, r) + 1] * c;
      if (l > p + 1e-3) return !1;
    }
    return m >= 1;
  }
  static evaluateVoronoiFrame(e, n, o, i = 1) {
    return bn(e, n, o, i);
  }
  static processFrame(e, n, o, i, s, a = !1, c = -1, l = !1) {
    var B, z;
    e.activeTime += s, e.doubleCommitCooldownSec > 0 && (e.doubleCommitCooldownSec = Math.max(0, e.doubleCommitCooldownSec - s));
    const m = n.meta.players, f = (((B = m[i]) == null ? void 0 : B.team) ?? 0) === 1, h = f ? 1 : 0, r = f ? 0 : 1, u = f ? 1 : -1, p = n.carKin, g = n.carKinBase(o, i), _ = p[g], b = p[g + 1], d = p[g + 2], E = p[g + 3], v = p[g + 4], S = p[g + 5], A = n.ballKinBase(o), M = n.ballKin[A], C = n.ballKin[A + 1], D = n.ballKin[A + 2], K = f ? b > Re : b < -Re, y = f ? b < -Re : b > Re, U = f ? b >= 0 : b <= 0, N = b * u >= 3500;
    K ? e.timeDefThird += s : y ? e.timeOffThird += s : e.timeNeutralThird += s, U ? e.timeDefHalf += s : e.timeOffHalf += s;
    const H = f ? b >= C : b <= C;
    H ? e.timeBehindBall += s : e.timeInFrontBall += s;
    const F = Math.hypot(_ - M, b - C, d - D);
    e.distBallIntegral += F * s, c === h ? (e.distBallPossIntegral += F * s, e.timePossession += s) : c === r && (e.distBallNoPossIntegral += F * s, e.timeNoPossession += s);
    const Y = b * -u;
    let Z = 0, V = 0, L = 0, W = 0, P = 0, ne = 0, x = 0, I = 0, G = 0, w = 0, R = 0, k = 0, Q = !1, X = F;
    for (let T = 0; T < n.carSlotCount; T++) {
      if (T === i || ((z = m[T]) == null ? void 0 : z.team) !== h) continue;
      const J = n.getCarFlagsLo(o, T);
      if ((J & O.ACTIVE) === 0 || (J & O.DEMOLISHED) !== 0) continue;
      const te = n.carKinBase(o, T), $ = p[te], ee = p[te + 1], oe = p[te + 2], se = ee * -u;
      se < Y - 1e-3 ? Z++ : se > Y + 1e-3 ? V++ : L++;
      const ie = Math.hypot($ - M, ee - C, oe - D);
      ie < X && (X = ie), ie < F - 1e-3 ? W++ : ie > F + 1e-3 ? P++ : ne++;
      const ue = Math.hypot(_ - $, b - ee, d - oe);
      if (k += ue, !l && ue <= 650 && F <= 800 && ie <= 800) {
        const Se = (E * (M - _) + v * (C - b) + S * (D - d)) / Math.max(1, F), Ee = (p[te + 3] * (M - $) + p[te + 4] * (C - ee) + p[te + 5] * (D - oe)) / Math.max(1, ie);
        (Se >= 200 && Ee >= 200 || F <= 350 && Ee >= 250 || ie <= 350 && Se >= 250) && (Q = !0);
      }
      R === 0 ? (x = $, I = ee) : R === 1 && (G = $, w = ee), R++;
    }
    if (e.hasPrevActive && N && !e.prevInDeepDefBox && v * u > 0 && C * u > 0 && (F > 900 || W > 0) && (e.totalDefEntries++, (M > 300 && _ >= -2200 && _ <= 250 || M < -300 && _ >= -250 && _ <= 2200 || Math.abs(M) <= 300 && Math.abs(_) >= 250 && Math.abs(_) <= 2200) && e.backpostEntries++), e.prevInDeepDefBox = N, Q ? e.doubleCommitSustainedSec += s : e.doubleCommitSustainedSec = 0, e.doubleCommitSustainedSec >= 0.15 - 1e-4 && e.doubleCommitCooldownSec <= 0 && (e.doubleCommits++, e.doubleCommitCooldownSec = 1.5, e.doubleCommitSustainedSec = 0, e.doubleCommitTimes.push(n.getTime(o))), R >= 1) {
      if (e.distMatesIntegral += k / R * s, e.timeWithMates += s, Z === 0) {
        const T = s / (1 + L);
        e.timeMostBack += T, e.thirdManTime += T, Y > 0 && (e.thirdManOverextensionIntegral += Y / 5120 * T);
      }
      V === 0 && (e.timeMostForward += s / (1 + L)), W === 0 && (e.timeClosestToBall += s / (1 + ne)), P === 0 && (e.timeFarthestFromBall += s / (1 + ne));
    }
    if (R === 2 && (e.spacingEntropyIntegral += Sn(_, b, x, I, G, w) * s, e.spacingEntropyTime += s), c === r && H && F >= 400 && F <= 2600 && v * u > 100) {
      e.shadowDefenseTotalTime += s;
      const T = -M, J = u * 5120 - C, te = Math.hypot(T, J), $ = Math.hypot(E, v);
      te > 0 && $ > 200 && (E * T + v * J) / ($ * te) >= 0.819 && (e.shadowDefenseCompliantTime += s);
    }
    a || Tn(e, n, o, i, 1);
    const le = Math.abs(M) <= 1200 && C * u >= 3800 && c === r && X > 500;
    e.hasPrevActive && le && !e.prevBallInBox && e.boxEntryConcessions++, e.prevBallInBox = le, e.hasPrevActive = !0;
  }
  static finalizePositioning(e) {
    const n = e.activeTime;
    return {
      avg_distance_to_ball: n > 1e-4 ? Number((e.distBallIntegral / n).toFixed(1)) : 0,
      avg_dist_ball_possession: e.timePossession > 1e-4 ? Number((e.distBallPossIntegral / e.timePossession).toFixed(1)) : 0,
      avg_dist_ball_no_possession: e.timeNoPossession > 1e-4 ? Number((e.distBallNoPossIntegral / e.timeNoPossession).toFixed(1)) : 0,
      avg_distance_to_mates: e.timeWithMates > 1e-4 ? Number((e.distMatesIntegral / e.timeWithMates).toFixed(1)) : 0,
      time_defensive_third: Number(e.timeDefThird.toFixed(1)),
      time_neutral_third: Number(e.timeNeutralThird.toFixed(1)),
      time_offensive_third: Number(e.timeOffThird.toFixed(1)),
      time_defensive_half: Number(e.timeDefHalf.toFixed(1)),
      time_offensive_half: Number(e.timeOffHalf.toFixed(1)),
      time_behind_ball: Number(e.timeBehindBall.toFixed(1)),
      time_infront_ball: Number(e.timeInFrontBall.toFixed(1)),
      time_most_back: Number(e.timeMostBack.toFixed(1)),
      time_most_forward: Number(e.timeMostForward.toFixed(1)),
      time_closest_to_ball: Number(e.timeClosestToBall.toFixed(1)),
      time_farthest_from_ball: Number(e.timeFarthestFromBall.toFixed(1)),
      goals_against_as_last_man: e.goalsAgainstAsLastMan
    };
  }
  static finalizeRotation(e, n) {
    const o = e.totalDefEntries > 0 ? e.backpostEntries / e.totalDefEntries * 100 : 100, i = e.thirdManTime > 1e-4 ? e.thirdManOverextensionIntegral / e.thirdManTime : 0, s = e.spacingEntropyTime > 1e-4 ? Number((e.spacingEntropyIntegral / e.spacingEntropyTime).toFixed(3)) : null, a = e.shadowDefenseTotalTime > 1e-4 ? e.shadowDefenseCompliantTime / e.shadowDefenseTotalTime * 100 : 100;
    let c = 0;
    for (const m of e.doubleCommitTimes)
      n.some((f) => f >= m && f <= m + 5) && c++;
    const l = e.doubleCommitTimes.length > 0 ? c / e.doubleCommitTimes.length * 100 : 0;
    return {
      backpost_rotation_rate: Number(o.toFixed(1)),
      double_commit_count: e.doubleCommits,
      double_commit_vulnerability: Number(l.toFixed(1)),
      third_man_overextension_index: Number(i.toFixed(3)),
      spacing_triangle_entropy: s,
      shadow_defense_compliance: Number(a.toFixed(1))
    };
  }
}
const En = [
  "SPEED_FLIP",
  "DIAGONAL",
  "STANDARD",
  "CHEAT",
  "CORNER_BOOST",
  "BACKBOARD_COVER",
  "FAKE"
];
class Ve {
  static computeValuation(e, n, o, i, s, a, c, l = !1, m) {
    const f = m !== void 0 && m > 1e-4 ? m : e.voronoiTotalEvaluations, h = f > 1e-4 ? e.voronoiPointsOwned / f * 100 : 0;
    let r = 0, u = 0, p = 0;
    const g = i === 0 ? 1 : -1;
    for (const v of s) {
      const S = (v.event_type ?? "").toLowerCase(), A = S === "demo" || S === "demolition";
      A && l && c && v.frame >= 0 && v.frame < c.length && c[v.frame] === 0 || ((v.player_slot === n || v.player_slot == null && v.player_name === o) && (S === "goal" ? r += me.goal : S === "shot" ? r += me.shot : S === "assist" ? r += me.assist : S === "epic_save" ? u += me.epic_save : S === "save" ? u += me.save : A && (r += me.demo)), A && (v.victim_slot === n || v.victim_slot == null && v.victim_name === o) && (p += me.demo_taken));
    }
    for (const v of a) {
      if (v.slot !== n) continue;
      const S = v.y * g, A = v.vy * g;
      if (A > 150) {
        const M = Math.min(0.22, A / 4e3 * 0.18 + v.impulse / 3500 * 0.08);
        S >= 0 ? r += M : u += M;
      } else A < -350 && S < 0 && (p += Math.min(0.12, Math.abs(A) / 5e3));
    }
    const _ = r + u - p, b = r + u, d = b > 1e-6 ? r / b * 100 : 0, E = b > 1e-6 ? 100 - Number(d.toFixed(1)) : 0;
    return {
      voronoi_territory_share: Number(h.toFixed(1)),
      cumulative_pva_integral: Number(_.toFixed(2)),
      offensive_pva_share: Number(d.toFixed(1)),
      defensive_pva_share: Number(E.toFixed(1)),
      box_entry_concessions: e.boxEntryConcessions
    };
  }
  static findKickoffFrames(e) {
    return ot(e);
  }
  static computeKickoffs(e, n, o) {
    var A, M, C, D;
    const i = e.frameCount, s = e.meta.players, a = (((A = s[n]) == null ? void 0 : A.team) ?? 0) === 1 ? 1 : 0, c = a === 1 ? 1 : -1, l = o ?? ot(e);
    let m = 0, f = 0, h = 0, r = 0, u = 0, p = 0, g = 0;
    const _ = {
      SPEED_FLIP: 0,
      DIAGONAL: 0,
      STANDARD: 0,
      CHEAT: 0,
      CORNER_BOOST: 0,
      BACKBOARD_COVER: 0,
      FAKE: 0
    }, b = e.meta.events.filter((K) => (K.event_type ?? "").toLowerCase() === "goal");
    for (let K = 0; K < l.length; K++) {
      const y = l[K], U = K + 1 < l.length ? e.getTime(l[K + 1]) : 1 / 0;
      if ((e.getCarFlagsLo(y, n) & O.ACTIVE) === 0) continue;
      const N = e.getTime(y), H = e.carKinBase(y, n), F = e.carKin[H], Y = e.carKin[H + 1], Z = Math.hypot(F, Y);
      let V = !0;
      for (let B = 0; B < e.carSlotCount; B++) {
        if (B === n || (((M = s[B]) == null ? void 0 : M.team) ?? 0) !== a || (e.getCarFlagsLo(y, B) & O.ACTIVE) === 0) continue;
        const z = e.carKinBase(y, B);
        Math.hypot(e.carKin[z], e.carKin[z + 1]) < Z - 50 && (V = !1);
      }
      const { hitFrame: L, winnerSlots: W } = rt(e, y), P = L >= 0 ? e.getTime(L) : -1, ne = W.includes(n);
      ne && (f += 1 / W.length);
      let x = Z, I = Z, G = 1 / 0, w = !1, R = 0, k = !1, Q = !1, X = !1;
      for (let B = y; B < i && !(B > y && e.getGameCountdown(B) > 0); B++) {
        const z = e.getTime(B), T = z - N;
        if (T > 10) break;
        const J = e.carKinBase(B, n), te = e.carKin[J], $ = e.carKin[J + 1], ee = Math.hypot(e.carKin[J + 3], e.carKin[J + 4], e.carKin[J + 5]);
        if (T <= 3 || L >= 0 && B <= L) {
          const oe = Math.hypot(te, $);
          oe < x && (x = oe), T <= 3 && (oe <= 260 && T < G && (G = T), Math.hypot(Math.abs(te) - 3072, $ - c * 4096) <= 420 && (k = !0)), (L < 0 || B <= L) && (oe < I && (I = oe), ee > R && (R = ee), (e.getCarFlagsLo(B, n) & O.DODGED) !== 0 && (w = !0));
        }
        if (!Q && L >= 0 && z - P >= 0.85) {
          Q = !0;
          const oe = e.ballKinBase(B);
          Math.hypot(e.ballKin[oe], e.ballKin[oe + 1]) < 380 ? g++ : e.ballKin[oe + 1] * -c > 350 ? u++ : e.ballKin[oe + 1] * c > 350 && p++;
        }
        if (B > y) {
          if (e.getGameBlueScore(B) > e.getGameBlueScore(y)) {
            a === 0 ? (h++, Q || u++) : (r++, Q || p++), X = !0;
            break;
          }
          if (e.getGameOrangeScore(B) > e.getGameOrangeScore(y)) {
            a === 1 ? (h++, Q || u++) : (r++, Q || p++), X = !0;
            break;
          }
        }
      }
      if (!X) {
        for (const B of b)
          if (B.frame >= y && B.timestamp >= N && B.timestamp <= N + 10 && B.timestamp < U) {
            const z = B.team ?? (B.player_slot != null ? (C = s[B.player_slot]) == null ? void 0 : C.team : (D = s.find((T) => T.name === B.player_name)) == null ? void 0 : D.team);
            (z === 0 || z === 1) && (z === a ? (h++, Q || u++) : (r++, Q || p++));
            break;
          }
      }
      if (L >= 0 ? ne || I <= 550 : x <= 950) {
        m++;
        const B = Z < 3500 ? 1.92 : Z < 4200 ? 2.08 : 2.25;
        G <= B || w && R >= 2180 && G <= B + 0.14 ? _.SPEED_FLIP++ : Math.abs(F) > 1500 ? _.DIAGONAL++ : _.STANDARD++;
      } else k ? _.CORNER_BOOST++ : V && Math.abs(Y) <= 2900 ? _.FAKE++ : x <= 2700 && Math.abs(Y) - x >= 800 ? _.CHEAT++ : _.BACKBOARD_COVER++;
    }
    let d = "STANDARD", E = 0;
    for (const K of En)
      _[K] > E && (E = _[K], d = K);
    let v = "NEUTRAL";
    r > h && r > 0 ? v = "DIRECT_GOAL_CONCEDED" : u > p && u >= g ? v = "ATTACKING_CORNER" : p > u && p >= g ? v = "OWN_CORNER" : g > 0 && (v = "DEAD_CENTER");
    const S = m > 0 ? Math.min(100, f / m * 100) : 0;
    return {
      kickoff_archetype: d,
      kickoff_first_touch_rate: Number(S.toFixed(1)),
      kickoff_deflection_quality: v,
      kickoff_goal_differential: h - r
    };
  }
}
class ut {
  static analyzeMatch(e) {
    var ne, x, I, G;
    const n = e.meta, o = e.frameCount, i = e.carSlotCount, a = 1 / Math.max(1, e.tickRateHz || n.record_fps || 30), c = Math.max(1, e.durationSeconds || n.duration_seconds || o * a), l = Ut.analyze(e, a), m = l.activeMatchDuration > 1e-4 ? l.activeMatchDuration : c, f = Array.from({ length: i }, () => be.createInitialState()), h = Array.from({ length: i }, () => Te.createInitialState()), r = Array.from({ length: i }, () => de.createInitialState()), u = Array.from({ length: i }, () => he.createInitialState()), p = new Float64Array(34), g = new Float32Array(260), _ = Ve.findKickoffFrames(e), b = mn(e, _), d = { carTouchByFrame: an(e), kickoffHitTimes: b.hitTimes };
    let E = 0, v = 0, S = 0, A = 0, M = 0;
    for (let w = 0; w < o; w++) {
      const R = l.dtByFrame[w];
      if (l.isFrameActive[w] === 0) {
        p.fill(0), g.fill(0);
        for (let T = 0; T < i; T++)
          be.resetContinuity(f[T]), Te.resetContinuity(h[T]), de.resetContinuity(r[T]), he.resetContinuity(u[T]);
        continue;
      }
      const k = l.possessingTeam[w];
      k === 0 ? E += R : k === 1 && (v += R);
      const Q = e.ballKin[e.ballKinBase(w) + 1];
      Q > 0 ? S += R : Q < 0 && (A += R), M += de.evaluateVoronoiFrame(r, e, w, R), he.evaluateFramePairs(u, g, e, w, R, d);
      const X = e.getTime(w), le = b.goTimes.some((T) => X >= T && X <= T + 2.5), B = l.blueAvgBoost[w] < 15 && Q < 0, z = l.orangeAvgBoost[w] < 15 && Q > 0;
      for (let T = 0; T < i; T++) {
        const J = e.getCarFlagsLo(w, T);
        if ((J & O.ACTIVE) === 0 || (J & O.DEMOLISHED) !== 0) {
          be.resetContinuity(f[T]), Te.resetContinuity(h[T]), de.resetContinuity(r[T]), he.resetContinuity(u[T]);
          continue;
        }
        const te = (((ne = n.players[T]) == null ? void 0 : ne.team) ?? 0) === 1, $ = e.carKinBase(w, T), ee = e.carKin, oe = ee[$], se = ee[$ + 1], ie = ee[$ + 2], ue = ee[$ + 3], Se = ee[$ + 4], Ee = ee[$ + 5], Ae = ee[$ + 6], Ce = ee[$ + 7], Me = ee[$ + 8], Be = ee[$ + 9], Ze = e.getCarBoostPct(w, T);
        h[T].hasPrevPos && (oe - h[T].prevX) ** 2 + (se - h[T].prevY) ** 2 + (ie - h[T].prevZ) ** 2 >= 1500 * 1500 && (Math.abs(Ze - 33.33) < 1.5 && be.resetContinuity(f[T]), Te.resetContinuity(h[T]), de.resetContinuity(r[T]), he.resetContinuity(u[T]));
        const ht = (J & O.ON_GROUND) !== 0, dt = (J & O.BOOST_FIRING) !== 0, pt = (J & O.SUPERSONIC) !== 0, gt = (J & O.DODGED) !== 0, Ne = e.getCarFlagsHi(w, T), Je = (Ne & Oe.HANDBRAKE) !== 0, vt = (Ne & Oe.PICKUP_BIG) !== 0 ? 2 : (Ne & Oe.PICKUP_SMALL) !== 0 ? 1 : 0, { speed: St, isSupersonic: bt, isEffectivelyGrounded: Tt } = Te.processSample(
          h[T],
          oe,
          se,
          ie,
          ue,
          Se,
          Ee,
          Ae,
          Ce,
          Me,
          Be,
          R,
          ht,
          pt,
          Je,
          gt
        ), yt = be.isSmoothHeadingAlignment(ue, Se, Ae, Ce, Me, Be, Je), Et = te ? B : z, At = be.processSample(
          f[T],
          Ze,
          oe,
          se,
          te,
          R,
          bt,
          St,
          yt,
          Et,
          ie,
          dt,
          p,
          X,
          vt
        );
        Te.recordAirBoost(h[T], At, Tt), de.processFrame(r[T], e, w, T, R, !0, k, le), he.processFrame(u[T], e, w, T, R, !0);
      }
    }
    if (o > 0)
      for (const w of n.events) {
        if ((w.event_type ?? "").toLowerCase() !== "goal") continue;
        const R = w.player_slot != null ? n.players[w.player_slot] : n.players.find((X) => X.name === w.player_name), k = w.team ?? (R == null ? void 0 : R.team);
        if (k !== 0 && k !== 1) continue;
        const Q = fn(e, w, k, l.touches, l.isFrameActive);
        for (let X = 0; X < i; X++)
          ((x = n.players[X]) == null ? void 0 : x.team) !== k && de.isMostBackAtFrame(e, Q, X) && r[X].goalsAgainstAsLastMan++;
      }
    const C = n.players.some((w) => w.is_mvp);
    let D = -1;
    if (!C && n.team_0_score !== n.team_1_score) {
      const w = n.team_0_score > n.team_1_score ? 0 : 1;
      let R = 0;
      for (let k = 0; k < n.players.length; k++)
        n.players[k].team === w && n.players[k].score > R && (R = n.players[k].score, D = k);
    }
    let K = 0, y = 0, U = 0, N = 0;
    const H = (I = n.shot_events) != null && I.length || (G = n.assist_events) != null && G.length ? [...n.events, ...n.shot_events ?? [], ...n.assist_events ?? []] : n.events, F = {}, Y = [];
    for (let w = 0; w < n.players.length; w++) {
      const R = n.players[w], k = R.team === 1, Q = k ? n.team_1_score : n.team_0_score, X = k ? n.team_0_score : n.team_1_score, le = n.players.reduce((Me, Be) => Be.team !== R.team ? Me + Be.shots : Me, 0);
      k ? y += R.shots : K += R.shots;
      const B = k ? l.team0ThreatTimes : l.team1ThreatTimes, z = R.is_mvp || w === D, T = rn.compute(R, Q, X, le, z, e, l.touches), J = be.finalize(f[w], m), te = Te.finalize(h[w], m), $ = de.finalizePositioning(r[w]), ee = he.finalizePhysicality(w, R.name, n.events, u[w], l.isFrameActive, l.hasCountdowns), oe = de.finalizeRotation(r[w], B), se = he.finalizeStarvation(f[w]), ie = he.finalizeMicroMechanics(h[w], u[w], R), ue = he.finalizeChallenges(u[w]), Se = Ve.computeValuation(
        r[w],
        w,
        R.name,
        R.team,
        H,
        l.touches,
        l.isFrameActive,
        l.hasCountdowns,
        M
      ), Ee = Ve.computeKickoffs(e, w, _), Ae = J.count_collected_big + J.count_collected_small;
      if (k ? N += Ae : U += Ae, F[R.name] && h[w].activeTime === 0) continue;
      const Ce = F[R.name] ? `${R.name} (${w + 1})` : R.name;
      M > 1e-4 && Y.push({ key: Ce, rawTenths: r[w].voronoiPointsOwned / M * 1e3 }), F[Ce] = {
        core: T,
        boost: J,
        movement: te,
        positioning: $,
        demo: ee,
        rotation: oe,
        starvation: se,
        micro_mechanics: ie,
        challenges: ue,
        valuation: Se,
        kickoffs: Ee
      };
    }
    if (M > 1e-4 && Y.length > 0) {
      const w = yn(Y);
      if (w)
        for (const [R, k] of Object.entries(w)) F[R].valuation.voronoi_territory_share = k;
    }
    const Z = E + v, V = S + A, L = Z > 1e-4 ? Number((E / Z * 100).toFixed(1)) : V > 1e-4 ? Number((S / V * 100).toFixed(1)) : 50, W = U + N, P = W > 0 ? Number((U / W * 100).toFixed(1)) : 50;
    return {
      match_guid: n.match_guid,
      map_name: n.map_name,
      duration_seconds: Number(m.toFixed(2)),
      team_0_score: n.team_0_score,
      team_1_score: n.team_1_score,
      players: F,
      team_stats: {
        blue: { possession_pct: L, shots: K, goals: n.team_0_score, pad_control_pct: P },
        orange: { possession_pct: Number((100 - L).toFixed(1)), shots: y, goals: n.team_1_score, pad_control_pct: Number((100 - P).toFixed(1)) }
      }
    };
  }
  static isPhysicalBallTouch(e, n, o, i = 88) {
    const s = e[0] - n[0], a = e[1] - n[1], c = e[2] - n[2], l = i + 92.75;
    return s * s + a * a + c * c > l * l ? !1 : o[0] ** 2 + o[1] ** 2 + o[2] ** 2 >= 22500;
  }
}
function An(t, e) {
  for (let n = 0; n < t.frameCount; n++)
    if ((t.getCarFlagsLo(n, e) & O.ACTIVE) !== 0 && t.getCarBoostPct(n, e) < 5)
      return Number(t.getTime(n).toFixed(2));
  return 0;
}
function Cn(t, e, n) {
  var a, c;
  const o = ((a = t.meta.players[e]) == null ? void 0 : a.team) ?? 0, i = t.carKin, s = t.ballKin;
  for (let l = 0; l < t.frameCount; l++) {
    const m = t.getCarFlagsLo(l, e);
    if ((m & O.ACTIVE) === 0 || (m & O.DEMOLISHED) !== 0) continue;
    const f = t.carKinBase(l, e), h = t.ballKinBase(l), r = Math.hypot(i[f] - s[h], i[f + 1] - s[h + 1], i[f + 2] - s[h + 2]);
    if (r > 800) continue;
    const u = (i[f + 3] * (s[h] - i[f]) + i[f + 4] * (s[h + 1] - i[f + 1]) + i[f + 5] * (s[h + 2] - i[f + 2])) / Math.max(1, r);
    for (let p = 0; p < t.carSlotCount; p++) {
      if (p === e || (((c = t.meta.players[p]) == null ? void 0 : c.team) ?? 0) !== o) continue;
      const g = t.getCarFlagsLo(l, p);
      if ((g & O.ACTIVE) === 0 || (g & O.DEMOLISHED) !== 0) continue;
      const _ = t.carKinBase(l, p), b = Math.hypot(i[f] - i[_], i[f + 1] - i[_ + 1], i[f + 2] - i[_ + 2]), d = Math.hypot(i[_] - s[h], i[_ + 1] - s[h + 1], i[_ + 2] - s[h + 2]);
      if (b > 650 || d > 800) continue;
      const E = (i[_ + 3] * (s[h] - i[_]) + i[_ + 4] * (s[h + 1] - i[_ + 1]) + i[_ + 5] * (s[h + 2] - i[_ + 2])) / Math.max(1, d);
      if (u >= 200 && E >= 200 || r <= 350 && E >= 250 || d <= 350 && u >= 250)
        return Number(t.getTime(l).toFixed(2));
    }
  }
  return n;
}
function Mn(t, e) {
  const n = t.ballKin, o = t.carKin;
  for (let i = 1; i < t.frameCount; i++) {
    const s = t.ballKinBase(i - 1), a = t.ballKinBase(i), c = t.carKinBase(i, e), l = Math.hypot(n[a + 3] - n[s + 3], n[a + 4] - n[s + 4], n[a + 5] - n[s + 5]), m = Math.hypot(o[c] - n[a], o[c + 1] - n[a + 1], o[c + 2] - n[a + 2]);
    if (l >= 250 && m <= 220) return Number(t.getTime(i).toFixed(2));
  }
  return t.frameCount > 0 ? Number(t.getTime(Math.floor(t.frameCount / 2)).toFixed(2)) : 0;
}
class Bn {
  static evaluate(e, n, o) {
    var h, r, u, p;
    const i = n ?? ut.analyzeMatch(e), s = o ?? He.extractMomentsFromStore(e), a = He.extractPlayerPeaksAndDips(s), c = [];
    let l = 1;
    const { isFrameActive: m, hasCountdowns: f } = ze(e);
    for (let g = 0; g < e.meta.players.length; g++) {
      const _ = e.meta.players[g], b = g > 0 && e.meta.players.slice(0, g).some((S) => S.name === _.name) ? `${_.name} (${g + 1})` : _.name, d = i.players[b] ?? i.players[_.name];
      if (!d) continue;
      const E = Mn(e, g), v = ((r = (h = a[_.name]) == null ? void 0 : h.peak) == null ? void 0 : r.timestamp) ?? E;
      d.boost.amount_used_supersonic >= 50 && c.push({
        id: `local-${l++}`,
        type: "boost_efficiency_warning",
        title: "Supersonic Boost Waste",
        category: "boost",
        timestamp: v,
        player_involved: _.name,
        description: `${_.name} spent ${d.boost.amount_used_supersonic} boost while already supersonic (2200+ UU/s). Release boost sooner after reaching max speed.`,
        context: { amount_used_supersonic: d.boost.amount_used_supersonic }
      }), d.boost.amount_overfill >= 25 && c.push({
        id: `local-${l++}`,
        type: "boost_overfill",
        title: "Orbital Big-Pad Overfill",
        category: "boost",
        timestamp: E,
        player_involved: _.name,
        description: `${_.name} overfilled ${d.boost.amount_overfill} boost on big pads. Route over small 12-boost pads when above 65%.`,
        context: { amount_overfill: d.boost.amount_overfill }
      }), d.boost.percent_zero_boost >= 15 && c.push({
        id: `local-${l++}`,
        type: "boost_starvation",
        title: "Extended Zero-Boost Windows",
        category: "boost",
        timestamp: An(e, g),
        player_involved: _.name,
        description: `${_.name} spent ${d.boost.percent_zero_boost}% of match time (${d.boost.time_zero_boost}s) empty on boost.`,
        context: { percent_zero_boost: d.boost.percent_zero_boost }
      }), d.rotation.double_commit_count > 0 && c.push({
        id: `local-${l++}`,
        type: "double_commit_alert",
        title: "Double-Commit Cluster",
        category: "rotation",
        timestamp: Cn(e, g, ((p = (u = a[_.name]) == null ? void 0 : u.dip) == null ? void 0 : p.timestamp) ?? E),
        player_involved: _.name,
        description: `${_.name} logged ${d.rotation.double_commit_count} double-commits within 650 UU of a teammate.`,
        context: { double_commit_count: d.rotation.double_commit_count }
      }), d.rotation.backpost_rotation_rate < 60 && d.positioning.time_defensive_third > 2 && c.push({
        id: `local-${l++}`,
        type: "backpost_discipline",
        title: "Front-Post Defensive Entries",
        category: "rotation",
        timestamp: E,
        player_involved: _.name,
        description: `${_.name} rotated backpost on ${d.rotation.backpost_rotation_rate}% of defensive third entries.`,
        context: { backpost_rotation_rate: d.rotation.backpost_rotation_rate }
      }), d.movement.percent_supersonic >= 12 && c.push({
        id: `local-${l++}`,
        type: "tempo_strength",
        title: "High Supersonic Rotation Tempo",
        category: "movement",
        timestamp: v,
        player_involved: _.name,
        description: `${_.name} maintained supersonic pace for ${d.movement.percent_supersonic}% (${d.movement.time_supersonic_speed}s) with avg ${d.movement.avg_speed} UU/s.`,
        context: { avg_speed: d.movement.avg_speed }
      });
    }
    for (const g of e.meta.events)
      g.event_type === "demo" && f && g.frame >= 0 && g.frame < m.length && m[g.frame] === 0 || (g.event_type === "goal" || g.event_type === "demo") && c.push({
        id: `local-${l++}`,
        type: g.event_type === "goal" ? "key_goal_sequence" : "physical_disruption",
        title: g.event_type === "goal" ? "Goal Conversion Sequence" : "Demolition Impact Window",
        category: g.event_type === "goal" ? "valuation" : "demo",
        timestamp: Number(g.timestamp.toFixed(2)),
        player_involved: g.player_name ?? void 0,
        description: g.victim_name ? `${g.player_name ?? "Player"} ${g.event_type} vs ${g.victim_name} at ${g.timestamp.toFixed(1)}s.` : `${g.player_name ?? "Player"} recorded ${g.event_type} at ${g.timestamp.toFixed(1)}s.`,
        linked_events: [`${g.event_type}@${g.frame}`]
      });
    if (c.length === 0 && e.meta.players.length > 0) {
      const g = e.meta.players[0], _ = i.players[g.name];
      c.push({
        id: `local-${l++}`,
        type: "baseline_summary",
        title: "Offline Match Telemetry Summary",
        category: "positioning",
        timestamp: 0,
        player_involved: g.name,
        description: `Evaluated ${e.frameCount} frames (${e.durationSeconds.toFixed(1)}s). ${g.name} averaged ${(_ == null ? void 0 : _.movement.avg_speed) ?? 0} UU/s and ${(_ == null ? void 0 : _.boost.avg_amount) ?? 0} boost.`
      });
    }
    return c.sort((g, _) => g.timestamp - _.timestamp);
  }
}
const wn = [
  "#2563eb",
  // Royal Blue
  "#00d4ff",
  // Electric Cyan-Blue
  "#6366f1",
  // Cobalt Indigo
  "#0284c7",
  // Cerulean Sky
  "#0d9488"
  // Deep Ocean Teal
], Dn = [
  "#f97316",
  // Flame Orange
  "#f59e0b",
  // Sunburst Amber
  "#ef4444",
  // Crimson Ember
  "#fb923c",
  // Tangerine Glow
  "#e11d48"
  // Coral Ruby
];
function In(t, e) {
  const n = t === 1 ? Dn : wn, o = (Math.trunc(e) % n.length + n.length) % n.length;
  return n[o];
}
const st = 260, Fn = 310, Ln = 0.24;
function Rn(t) {
  const e = [], n = /* @__PURE__ */ new Map(), o = [0, 0];
  if (t)
    for (let i = 0; i < t.length; i++) {
      const s = t[i], a = (s == null ? void 0 : s.team) === 1 ? 1 : 0, c = In(a, o[a]++);
      e.push(c), s != null && s.name && n.set(s.name, c);
    }
  return { bySlot: e, byName: n };
}
function xn(t, e) {
  if (t.frameCount <= 0) return { pos: [0, 0, 93], quat: [0, 0, 0, 1] };
  const n = Math.max(0, Math.min(t.frameCount - 1, e)), o = t.ballKinBase(n), i = t.ballKin;
  return {
    pos: [i[o], i[o + 1], i[o + 2]],
    quat: [i[o + 6], i[o + 7], i[o + 8], i[o + 9]]
  };
}
function On(t, e, n, o) {
  if (t.frameCount <= 0 || t.carSlotCount <= 0) return [0, 0, 1];
  const i = Math.max(0, Math.min(t.frameCount - 1, e)), s = t.carKin;
  let a = typeof o == "number" && o >= 0 && o < t.carSlotCount ? o : -1, c = 450, l = 0, m = 0, f = 0;
  if (a >= 0) {
    const h = t.getCarFlagsLo(i, a);
    if ((h & O.ACTIVE) !== 0 && (h & O.DEMOLISHED) === 0) {
      const r = t.carKinBase(i, a);
      l = s[r], m = s[r + 1], f = s[r + 2];
      const u = Math.hypot(n[0] - l, n[1] - m, n[2] - f);
      if (u > 1e-3 && u <= 450)
        return [(n[0] - l) / u, (n[1] - m) / u, (n[2] - f) / u];
    }
  }
  a = -1;
  for (let h = 0; h < t.carSlotCount; h++) {
    const r = t.getCarFlagsLo(i, h);
    if ((r & O.ACTIVE) === 0 || (r & O.DEMOLISHED) !== 0) continue;
    const u = t.carKinBase(i, h), p = Math.hypot(n[0] - s[u], n[1] - s[u + 1], n[2] - s[u + 2]);
    p < c && (c = p, a = h, l = s[u], m = s[u + 1], f = s[u + 2]);
  }
  return a >= 0 && c > 1e-3 ? [(n[0] - l) / c, (n[1] - m) / c, (n[2] - f) / c] : [0, 0, 1];
}
function kn(t) {
  var f, h, r, u, p, g, _, b, d, E;
  const e = [], n = Rn((f = t.meta) == null ? void 0 : f.players), o = (r = (h = t.meta) == null ? void 0 : h.shot_events) != null && r.length ? [...t.meta.events ?? [], ...t.meta.shot_events] : ((u = t.meta) == null ? void 0 : u.events) ?? [];
  for (const v of o) {
    const S = (v.event_type ?? "").toLowerCase();
    if (S.includes("touch") || S.includes("hit") || S.includes("shot") || S.includes("save")) {
      const A = typeof v.frame == "number" ? v.frame : t.findFrameIndex(v.timestamp), M = xn(t, A), C = (typeof v.player_slot == "number" ? n.bySlot[v.player_slot] : void 0) ?? (v.player_name ? n.byName.get(v.player_name) : void 0) ?? null;
      e.push({
        event_type: v.event_type,
        timestamp: v.timestamp,
        frame: A,
        player_slot: v.player_slot,
        player_name: v.player_name,
        victim_slot: v.victim_slot,
        victim_name: v.victim_name,
        team: v.team,
        colorHex: C,
        details: v.details,
        pos: M.pos,
        normal: On(t, A, M.pos, v.player_slot),
        ballPos: M.pos,
        ballQuat: M.quat
      });
    }
  }
  if (t.frameCount < 3 || t.carSlotCount <= 0)
    return e.sort((v, S) => v.timestamp - S.timestamp);
  const i = t.ballKin, s = t.carKin, a = t.times, c = t.carSlotCount, l = ((p = t.meta) == null ? void 0 : p.players) ?? [], m = new Float64Array(c).fill(-999);
  for (let v = 1; v < t.frameCount; v++) {
    if (t.getGameCountdown(v) > 0 || (t.getGameFlags(v) & fe.MATCH_ENDED) !== 0) continue;
    const S = a[v - 1], A = a[v], M = A - S;
    if (!(M > 1e-4 && M < 0.25)) continue;
    const C = t.ballKinBase(v - 1), D = t.ballKinBase(v);
    if ((i[D] - i[C]) ** 2 + (i[D + 1] - i[C + 1]) ** 2 + (i[D + 2] - i[C + 2]) ** 2 > 2500 * 2500) continue;
    const y = i[D + 3] - i[C + 3], U = i[D + 4] - i[C + 4], N = i[D + 5] - (i[C + 5] - 650 * M);
    let H = Math.hypot(y, U, N);
    if (H < st && M < 0.025) {
      let B = v - 1;
      for (; B > 0 && A - a[B - 1] <= 0.036 && t.getGameCountdown(B - 1) === 0; ) {
        const z = t.ballKinBase(B - 1), T = t.ballKinBase(B);
        if ((i[T] - i[z]) ** 2 + (i[T + 1] - i[z + 1]) ** 2 + (i[T + 2] - i[z + 2]) ** 2 > 1500 * 1500) break;
        B--;
      }
      if (B < v - 1) {
        const z = t.ballKinBase(B), T = Math.max(M, A - a[B]);
        H = Math.max(H, Math.hypot(i[D + 3] - i[z + 3], i[D + 4] - i[z + 4], i[D + 5] - (i[z + 5] - 650 * T)));
      }
    }
    if (H < st) continue;
    const F = i[D], Y = i[D + 1], Z = i[D + 2], V = t.getLastTouchSlot(v);
    let L = -1, W = Fn, P = 0, ne = 0, x = 0, I = -1, G = 0, w = 0, R = 0;
    for (let B = 0; B < c; B++) {
      const z = t.getCarFlagsLo(v, B);
      if ((z & O.ACTIVE) === 0 || (z & O.DEMOLISHED) !== 0) continue;
      const T = t.carKinBase(v, B), J = s[T], te = s[T + 1], $ = s[T + 2], ee = Math.hypot(F - J, Y - te, Z - $);
      ee < W - 1e-3 ? (W = ee, L = B, P = J, ne = te, x = $, I = -1) : Math.abs(ee - W) <= 1e-3 && (B === V || L < 0 ? (W = ee, L = B, P = J, ne = te, x = $, I = -1) : L !== V && (((g = l[B]) == null ? void 0 : g.team) ?? 0) !== (((_ = l[L]) == null ? void 0 : _.team) ?? 0) && (I = B, G = J, w = te, R = $));
    }
    if (L >= 0 && I >= 0) {
      const B = i[D + 4], z = (((b = l[L]) == null ? void 0 : b.team) ?? 0) === 1;
      B > 1 && z || B < -1 && !z ? (L = I, P = G, ne = w, x = R) : Math.abs(B) <= 1 && (L = -1);
    }
    if (L < 0 || A - m[L] < Ln || e.some((B) => B.player_slot === L && Math.abs(B.timestamp - A) < 0.1))
      continue;
    m[L] = A;
    const k = 1 / Math.max(1e-3, W), Q = ((d = l[L]) == null ? void 0 : d.team) === 1 ? 1 : 0, X = [F, Y, Z], le = [i[D + 6], i[D + 7], i[D + 8], i[D + 9]];
    e.push({
      event_type: "touch",
      timestamp: A,
      frame: v,
      player_slot: L,
      player_name: ((E = l[L]) == null ? void 0 : E.name) ?? null,
      team: Q,
      colorHex: n.bySlot[L] ?? null,
      pos: X,
      normal: [(F - P) * k, (Y - ne) * k, (Z - x) * k],
      ballPos: X,
      ballQuat: le
    });
  }
  return e.sort((v, S) => v.timestamp - S.timestamp);
}
const _t = [
  ["score", "Score", "core", (t) => t.core.direct.score, "integer", "pts"],
  ["goals", "Goals", "core", (t) => t.core.direct.goals, "integer", ""],
  ["assists", "Assists", "core", (t) => t.core.direct.assists, "integer", ""],
  ["saves", "Saves", "core", (t) => t.core.direct.saves, "integer", ""],
  ["epic_saves", "Epic Saves", "core", (t) => t.core.direct.epic_saves ?? "N/A", "integer", "", "Server PRI MatchEpicSaves (N/A on pre-Sep-2026 replays)."],
  ["shots", "Shots", "core", (t) => t.core.direct.shots, "integer", ""],
  ["clears", "Clears", "core", (t) => t.core.direct.clears ?? "N/A", "integer", "", "Server PRI MatchClears (N/A on pre-Sep-2026 replays)."],
  ["centers", "Centers", "core", (t) => t.core.direct.centers ?? "N/A", "integer", "", "Server PRI MatchCenters (N/A on pre-Sep-2026 replays)."],
  ["shooting_pct", "Shooting %", "core", (t) => t.core.derived.shooting_percentage, "percentage", "%"],
  ["goal_part", "Goal Participation", "core", (t) => t.core.derived.goal_participation, "percentage", "%"],
  ["xg", "Expected Goals (xG)", "core", (t) => t.core.derived.xg, "decimal", "xG"],
  ["bpm", "Boost/Min (BPM)", "boost", (t) => t.boost.bpm, "decimal", "bpm"],
  ["bcpm", "Boost Collected/Min (BCPM)", "boost", (t) => t.boost.bcpm, "decimal", "bcpm"],
  ["avg_boost", "Avg Boost Amount", "boost", (t) => t.boost.avg_amount, "decimal", ""],
  ["amount_collected", "Boost Collected", "boost", (t) => t.boost.amount_collected, "integer", "b"],
  ["amount_stolen", "Boost Stolen", "boost", (t) => t.boost.amount_stolen, "integer", "b"],
  ["count_collected_big", "Big Pads Collected", "boost", (t) => t.boost.count_collected_big, "integer", ""],
  ["count_collected_small", "Small Pads Collected", "boost", (t) => t.boost.count_collected_small, "integer", ""],
  ["count_stolen_big", "Big Pads Stolen", "boost", (t) => t.boost.count_stolen_big, "integer", ""],
  ["count_stolen_small", "Small Pads Stolen", "boost", (t) => t.boost.count_stolen_small, "integer", ""],
  ["time_zero_boost", "Time at 0 Boost", "boost", (t) => t.boost.time_zero_boost, "duration", "s"],
  ["time_full_boost", "Time at 100 Boost", "boost", (t) => t.boost.time_full_boost, "duration", "s"],
  ["avg_speed", "Avg Speed", "movement", (t) => t.movement.avg_speed, "decimal", "UU/s"],
  ["total_distance", "Total Distance", "movement", (t) => t.movement.total_distance, "decimal", "UU"],
  ["time_supersonic", "Supersonic Time", "movement", (t) => t.movement.time_supersonic_speed, "duration", "s"],
  ["pct_supersonic", "Supersonic %", "movement", (t) => t.movement.percent_supersonic, "percentage", "%"],
  ["time_boost_speed", "Boost Speed Time", "movement", (t) => t.movement.time_boost_speed, "duration", "s"],
  ["time_slow_speed", "Slow Speed Time", "movement", (t) => t.movement.time_slow_speed, "duration", "s"],
  ["time_ground", "Ground Time", "movement", (t) => t.movement.time_ground, "duration", "s"],
  ["time_low_air", "Low Aerial Time", "movement", (t) => t.movement.time_low_air, "duration", "s"],
  ["time_high_air", "High Aerial Time", "movement", (t) => t.movement.time_high_air, "duration", "s"],
  ["time_powerslide", "Powerslide Time", "movement", (t) => t.movement.time_powerslide, "duration", "s"],
  ["count_powerslide", "Powerslide Count", "movement", (t) => t.movement.count_powerslide, "integer", ""],
  ["avg_dist_ball", "Avg Distance to Ball", "positioning", (t) => t.positioning.avg_distance_to_ball, "decimal", "UU"],
  ["avg_dist_ball_poss", "Avg Dist (Possession)", "positioning", (t) => t.positioning.avg_dist_ball_possession, "decimal", "UU"],
  ["avg_dist_ball_no_poss", "Avg Dist (Defending)", "positioning", (t) => t.positioning.avg_dist_ball_no_possession, "decimal", "UU"],
  ["avg_dist_mates", "Avg Distance to Teammates", "positioning", (t) => t.positioning.avg_distance_to_mates, "decimal", "UU"],
  ["time_def_third", "Defensive Third Time", "positioning", (t) => t.positioning.time_defensive_third, "duration", "s"],
  ["time_mid_third", "Neutral Third Time", "positioning", (t) => t.positioning.time_neutral_third, "duration", "s"],
  ["time_off_third", "Offensive Third Time", "positioning", (t) => t.positioning.time_offensive_third, "duration", "s"],
  ["time_def_half", "Defensive Half Time", "positioning", (t) => t.positioning.time_defensive_half, "duration", "s"],
  ["time_off_half", "Offensive Half Time", "positioning", (t) => t.positioning.time_offensive_half, "duration", "s"],
  ["time_behind_ball", "Time Behind Ball", "positioning", (t) => t.positioning.time_behind_ball, "duration", "s"],
  ["time_infront_ball", "Time Infront of Ball", "positioning", (t) => t.positioning.time_infront_ball, "duration", "s"],
  ["demo_inflicted", "Demos Inflicted", "demo", (t) => t.demo.inflicted, "integer", ""],
  ["demo_taken", "Demos Taken", "demo", (t) => t.demo.taken, "integer", ""],
  ["bumps_inflicted", "Bumps Inflicted", "demo", (t) => t.demo.bumps_inflicted, "integer", ""],
  ["bumps_taken", "Bumps Taken", "demo", (t) => t.demo.bumps_taken, "integer", ""],
  ["backpost_rate", "Backpost Rotation Rate", "rotation", (t) => t.rotation.backpost_rotation_rate, "percentage", "%"],
  ["double_commits", "Double Commit Count", "rotation", (t) => t.rotation.double_commit_count, "integer", ""],
  ["double_commit_vuln", "Double Commit Vulnerability", "rotation", (t) => t.rotation.double_commit_vulnerability, "percentage", "%"],
  ["overextension_idx", "Overextension Index", "rotation", (t) => t.rotation.third_man_overextension_index, "decimal", ""],
  ["spacing_entropy", "Spacing Triangle Entropy", "rotation", (t) => t.rotation.spacing_triangle_entropy ?? "N/A", "decimal", "", "3v3 team triangle spacing balance (N/A in 1v1/2v2)."],
  ["shadow_defense_rate", "Shadow Defense Compliance", "rotation", (t) => t.rotation.shadow_defense_compliance, "percentage", "%"],
  ["spi", "Starvation Pressure Index (SPI)", "starvation", (t) => t.starvation.starvation_pressure_index, "percentage", "%"],
  ["energy_conv", "Kinetic Conversion", "starvation", (t) => t.starvation.kinetic_energy_conversion, "decimal", ""],
  ["pad_path_eff", "Pad Pathing Efficiency", "starvation", (t) => t.starvation.pad_pathing_efficiency, "percentage", "%"],
  ["starve_time_inflicted", "Starvation Inflicted Time (Team)", "starvation", (t) => t.starvation.boost_starvation_time_inflicted, "duration", "s", "Team-level duration opponents averaged <15 boost while ball was in their half."],
  ["flip_resets", "Flip Reset Count", "micro_mechanics", (t) => t.micro_mechanics.flip_reset_count, "integer", ""],
  ["flip_reset_delay", "Avg Flip Reset Delay", "micro_mechanics", (t) => t.micro_mechanics.flip_reset_delay_avg, "decimal", "s"],
  ["landing_quality", "Landing Quality Score", "micro_mechanics", (t) => t.micro_mechanics.landing_recovery_quality, "decimal", "/100"],
  ["aerial_energy_eff", "Aerial Energy Efficiency", "micro_mechanics", (t) => t.micro_mechanics.aerial_energy_efficiency, "decimal", ""],
  ["wavedashes", "Wavedash Count", "micro_mechanics", (t) => t.micro_mechanics.wavedash_count, "integer", ""],
  ["aerial_hits", "Aerial Hits", "micro_mechanics", (t) => t.micro_mechanics.aerial_hits ?? "N/A", "integer", "", "Server PRI MatchAerialHits (N/A on pre-Sep-2026 replays)."],
  ["crossbar_hits", "Crossbar Hits", "micro_mechanics", (t) => t.micro_mechanics.crossbar_hits ?? "N/A", "integer", "", "Server PRI MatchCrossbarHits (N/A on pre-Sep-2026 replays)."],
  ["challenges_contested", "Challenges Contested", "challenges", (t) => t.challenges.challenges_contested, "integer", ""],
  ["challenges_won", "Challenges Won", "challenges", (t) => t.challenges.challenges_won, "integer", ""],
  ["challenges_lost", "Challenges Lost", "challenges", (t) => t.challenges.challenges_lost, "integer", ""],
  ["challenge_win_rate", "Challenge Win Rate", "challenges", (t) => t.challenges.challenge_win_rate, "percentage", "%"],
  ["time_to_challenge", "Challenge Reaction Latency", "challenges", (t) => t.challenges.time_to_challenge_latency, "decimal", "ms"],
  ["voronoi_share", "Voronoi Territory Share", "valuation", (t) => t.valuation.voronoi_territory_share, "percentage", "%"],
  ["pva_integral", "Cumulative PVA Integral", "valuation", (t) => t.valuation.cumulative_pva_integral, "decimal", "PVA"],
  ["offensive_pva", "Offensive PVA Share", "valuation", (t) => t.valuation.offensive_pva_share, "percentage", "%"],
  ["defensive_pva", "Defensive PVA Share", "valuation", (t) => t.valuation.defensive_pva_share, "percentage", "%"],
  ["box_entries_conceded", "Box Entries Conceded (Team)", "valuation", (t) => t.valuation.box_entry_concessions, "integer", "", "Team-level count of opponent-possessed ball entries into defensive box with no defender within 500 UU."],
  ["kickoff_archetype", "Kickoff Archetype", "kickoffs", (t) => t.kickoffs.kickoff_archetype, "string", ""],
  ["kickoff_first_touch", "Kickoff First Touch Rate", "kickoffs", (t) => t.kickoffs.kickoff_first_touch_rate, "percentage", "%"],
  ["kickoff_deflection", "Deflection Quality", "kickoffs", (t) => t.kickoffs.kickoff_deflection_quality, "string", ""],
  ["kickoff_goal_diff", "Kickoff Goal Differential", "kickoffs", (t) => t.kickoffs.kickoff_goal_differential, "integer", ""]
];
_t.map((t) => t[0]);
function Nn(t) {
  const e = {};
  for (const [n, o] of Object.entries(t.players)) {
    const i = {};
    for (const [s, a, c, l, m, f, h] of _t)
      i[s] = h ? { label: a, category: c, value: l(o), display_format: m, unit: f, description: h } : { label: a, category: c, value: l(o), display_format: m, unit: f };
    e[n] = i;
  }
  return e;
}
function ct(t) {
  const e = ut.analyzeMatch(t), n = Nn(e), o = He.extractMomentsFromStore(t), i = Bn.evaluate(t, e, o), s = kn(t);
  return { report: e, metrics: n, pvaMoments: o, localInsights: i, touchEvents: s };
}
const lt = /* @__PURE__ */ new Map();
function Kn(t) {
  const e = typeof t == "string" || t instanceof URL ? String(t) : "__default__";
  if (typeof t == "function" || t instanceof ArrayBuffer || ArrayBuffer.isView(t))
    return new qe(t);
  let n = lt.get(e);
  return n || (n = new qe(t), lt.set(e, n)), n;
}
async function Pn(t) {
  if (t.type === "PING")
    return { response: { type: "PONG", requestId: t.requestId }, transfer: [] };
  try {
    if (t.type === "PARSE_REPLAY") {
      const l = await Kn(t.wasmSource).parseReplay(t.replayBytes), m = new ke(l.buffer), f = ct(m);
      return {
        response: {
          type: "INGEST_READY",
          requestId: t.requestId,
          buffer: l.buffer,
          frameCount: l.header.frameCount,
          carSlotCount: l.header.carSlotCount,
          tickRateHz: l.header.tickRateHz,
          parseTimeMs: l.parseTimeMs,
          report: f.report,
          metrics: f.metrics,
          pvaMoments: f.pvaMoments,
          localInsights: f.localInsights,
          touchEvents: f.touchEvents
        },
        transfer: [l.buffer]
      };
    }
    const e = performance.now(), n = typeof t.payload == "string" ? JSON.parse(t.payload) : t.payload, o = Pt(n), i = We(o), s = new ke(o), a = ct(s);
    return {
      response: {
        type: "INGEST_READY",
        requestId: t.requestId,
        buffer: o,
        frameCount: i.frameCount,
        carSlotCount: i.carSlotCount,
        tickRateHz: i.tickRateHz,
        parseTimeMs: performance.now() - e,
        report: a.report,
        metrics: a.metrics,
        pvaMoments: a.pvaMoments,
        localInsights: a.localInsights,
        touchEvents: a.touchEvents
      },
      transfer: [o]
    };
  } catch (e) {
    const n = e instanceof Error ? e.message : String(e);
    return {
      response: { type: "INGEST_ERROR", requestId: t.requestId, error: n },
      transfer: []
    };
  }
}
const Ge = typeof self < "u" && typeof self.postMessage == "function" ? self : null;
Ge && (Ge.onmessage = (t) => {
  Pn(t.data).then(({ response: e, transfer: n }) => {
    Ge.postMessage(e, n);
  });
});
//# sourceMappingURL=ingest.worker-Bait9IP0.js.map

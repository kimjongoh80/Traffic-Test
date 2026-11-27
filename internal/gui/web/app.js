(() => {
  'use strict';
  const $ = id => document.getElementById(id);

  // ---------- formatting ----------
  const fmtBits = (b, precise) => {
    if (b == null || !isFinite(b)) return '-';
    const d = precise ? 2 : (b >= 100e9 || (b >= 100e6 && b < 1e9) ? 0 : 1);
    if (b >= 1e9) return (b / 1e9).toFixed(d === 0 && b < 10e9 ? 1 : d) + ' Gbps';
    if (b >= 1e6) return (b / 1e6).toFixed(d) + ' Mbps';
    if (b >= 1e3) return (b / 1e3).toFixed(d) + ' Kbps';
    return Math.round(b) + ' bps';
  };
  const fmtAxisBits = v => v === 0 ? '0' : v >= 1e9 ? +(v / 1e9).toFixed(2) + 'G' : v >= 1e6 ? +(v / 1e6).toFixed(1) + 'M' : v >= 1e3 ? +(v / 1e3).toFixed(1) + 'K' : String(Math.round(v));
  const fmtMs = (v, digits = 2) => v == null || !isFinite(v) ? '-' : v.toFixed(v >= 100 ? 0 : digits) + ' ms';
  const fmtPct = v => v == null || !isFinite(v) ? '-' : (v === 0 ? '0' : v < 0.01 ? v.toFixed(4) : v.toFixed(v < 1 ? 3 : 2)) + '%';
  const fmtBytes = b => b >= 2 ** 30 ? (b / 2 ** 30).toFixed(2) + ' GiB' : b >= 2 ** 20 ? (b / 2 ** 20).toFixed(1) + ' MiB' : b >= 1024 ? (b / 1024).toFixed(1) + ' KiB' : b + ' B';
  const fmtInt = n => (n ?? 0).toLocaleString('ko-KR');
  const fmtTime = t => new Date(t).toLocaleString('ko-KR', { hour12: false });
  const esc = s => String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

  const store = {
    get(k, d) { try { const v = localStorage.getItem('ttest.' + k); return v == null ? d : JSON.parse(v); } catch { return d; } },
    set(k, v) { try { localStorage.setItem('ttest.' + k, JSON.stringify(v)); } catch { } },
  };

  function toast(msg, ms = 3500) {
    const t = $('toast');
    t.textContent = msg; t.hidden = false;
    clearTimeout(toast._t); toast._t = setTimeout(() => t.hidden = true, ms);
  }

  async function api(path, body, method) {
    const opt = body !== undefined
      ? { method: method || 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }
      : { method: method || 'GET' };
    const r = await fetch(path, opt);
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(j.error || r.statusText);
    return j;
  }

  // ---------- presets ----------
  const PRESETS = [
    { name: '1G TCP 최대', p: { proto: 'tcp', streams: 4, bw: '', duration: 30, length: '' }, c: { bw: 900 } },
    { name: '1G UDP 손실', p: { proto: 'udp', streams: 1, bw: 950, unit: '1e6', duration: 30, length: 1460 }, c: { loss: 0.01, jit: 1 } },
    { name: '10G TCP', p: { proto: 'tcp', streams: 8, bw: '', duration: 30, length: '' }, c: { bw: 9000 } },
    { name: '25G TCP', p: { proto: 'tcp', streams: 8, bw: '', duration: 30, length: '' }, c: { bw: 23000 } },
    { name: '25G UDP 점보', p: { proto: 'udp', streams: 8, bw: 25, unit: '1e9', duration: 30, length: 8972 }, c: { loss: 0.1 } },
    { name: '지연 측정', p: { proto: 'udp', streams: 1, bw: 10, unit: '1e6', duration: 60, length: 1460, latency: 50 }, c: { rtt: 5, jit: 1 } },
    { name: '장시간 안정성', p: { proto: 'tcp', streams: 4, bw: 800, unit: '1e6', duration: 600, length: '' }, c: { bw: 750 } },
  ];

  // ---------- form ----------
  const seg = (id, v) => {
    const el = $(id);
    if (v !== undefined) el.querySelectorAll('button').forEach(b => b.classList.toggle('on', b.dataset.v === v));
    return el.querySelector('button.on').dataset.v;
  };
  document.querySelectorAll('.seg').forEach(el => el.addEventListener('click', e => {
    if (e.target.tagName !== 'BUTTON') return;
    seg(el.id, e.target.dataset.v);
    if (el.id === 'proto') {
      // buffer sizes do not carry over between TCP and UDP
      const len = parseInt($('length').value, 10);
      if (e.target.dataset.v === 'tcp' && len > 0 && len < 16384) $('length').value = '';
      if (e.target.dataset.v === 'udp' && len > 65507) $('length').value = '';
    }
    updateFormHints(); saveForm();
  }));

  function updateFormHints() {
    const udp = seg('proto') === 'udp';
    $('bwHint').textContent = udp ? '비우면 1 Gbps' : '비우면 최대 속도';
    $('lenLabel').textContent = udp ? '패킷 크기 (바이트)' : '버퍼 크기 (바이트)';
    $('length').placeholder = udp ? '1460' : '128K';
  }

  const formFields = ['host', 'port', 'duration', 'streams', 'bw', 'bwUnit', 'length', 'latency', 'cBw', 'cLoss', 'cRtt', 'cJit'];
  function saveForm() {
    const f = { proto: seg('proto'), dir: seg('dir') };
    formFields.forEach(k => f[k] = $(k).value);
    store.set('form', f);
  }
  function loadForm() {
    const f = store.get('form', null);
    if (!f) return;
    formFields.forEach(k => { if (f[k] != null) $(k).value = f[k]; });
    if (f.proto) seg('proto', f.proto);
    if (f.dir) seg('dir', f.dir);
  }
  formFields.forEach(k => $(k).addEventListener('change', saveForm));

  function applyPreset(pr) {
    const p = pr.p, c = pr.c || {};
    seg('proto', p.proto);
    $('streams').value = p.streams;
    $('bw').value = p.bw;
    if (p.unit) $('bwUnit').value = p.unit;
    $('duration').value = p.duration;
    $('length').value = p.length;
    $('latency').value = p.latency || 100;
    $('cBw').value = c.bw ?? ''; $('cLoss').value = c.loss ?? ''; $('cRtt').value = c.rtt ?? ''; $('cJit').value = c.jit ?? '';
    if (Object.keys(c).length) $('criteriaBox').open = true;
    updateFormHints(); saveForm();
    toast(`프리셋 적용: ${pr.name}`, 1800);
  }
  PRESETS.forEach(pr => {
    const b = document.createElement('button');
    b.className = 'chip'; b.textContent = pr.name;
    b.onclick = () => applyPreset(pr);
    $('presets').appendChild(b);
  });

  function readForm() {
    const num = id => { const v = parseFloat($(id).value); return isFinite(v) ? v : 0; };
    const params = {
      protocol: seg('proto'),
      reverse: seg('dir') === 'down',
      duration_sec: num('duration') || 10,
      streams: Math.max(1, Math.round(num('streams')) || 1),
      bandwidth: Math.round(num('bw') * parseFloat($('bwUnit').value)),
      length: Math.round(num('length')),
      interval_sec: 1,
      latency_ms: Math.round(num('latency')),
    };
    const criteria = {
      min_bits_per_sec: num('cBw') * 1e6,
      max_loss_pct: num('cLoss'),
      check_loss: $('cLoss').value.trim() !== '',
      max_rtt_ms: num('cRtt'),
      max_jitter_ms: num('cJit'),
    };
    return { host: $('host').value.trim(), port: Math.round(num('port')) || 5210, params, criteria };
  }

  // ---------- charts ----------
  const charts = {
    bw: new LineChart($('chBw'), { series: [{ name: '합계', color: '--s1' }], yFormat: (v, tip) => tip ? fmtBits(v, true) : fmtAxisBits(v) }),
    rtt: new LineChart($('chRtt'), { series: [{ name: '평균', color: '--s1' }, { name: '최대', color: '--s2', width: 1.5 }], yFormat: (v, tip) => tip ? fmtMs(v) : +v.toFixed(2) + '' }),
    jit: new LineChart($('chJit'), { series: [{ name: '지터', color: '--s3' }], yFormat: (v, tip) => tip ? fmtMs(v, 3) : +v.toFixed(3) + '' }),
    loss: new LineChart($('chLoss'), { series: [{ name: '손실률', color: '--s4' }], yFormat: (v, tip) => tip ? fmtPct(v) : +v.toFixed(3) + '%' }),
  };

  // ---------- view state ----------
  let view = null; // {id, params, criteria, intervals, startedAt, live, record}
  let liveTimer = null;

  function resetView(id, params, criteria, startedAt, live) {
    view = { id, params, criteria: criteria || {}, intervals: [], startedAt: startedAt ? new Date(startedAt) : new Date(), live, record: null };
    const udp = params.protocol === 'udp';
    charts.bw.setSeries([{ name: '합계', color: '--s1' }]);
    charts.rtt.setSeries([{ name: '평균', color: '--s1' }, { name: '최대', color: '--s2', width: 1.5 }]);
    charts.jit.setSeries(udp
      ? [{ name: 'UDP 지터', color: '--s3' }, { name: 'RTT 지터', color: '--s2', width: 1.5, dash: [4, 3] }]
      : [{ name: 'RTT 지터', color: '--s3' }]);
    charts.loss.setSeries(udp
      ? [{ name: '패킷 손실률', color: '--s4' }]
      : [{ name: 'RTT 프로브 손실률', color: '--s4' }]);
    $('chJitTitle').textContent = udp ? '지터 (UDP · RTT)' : '지터 (RTT 변동)';
    $('chLossTitle').textContent = udp ? '패킷 손실' : '프로브 손실 (TCP는 재전송으로 손실이 보정됨)';
    Object.values(charts).forEach(c => c.opts.xMax = params.duration_sec);
    const c = view.criteria;
    charts.bw.setThreshold(c.min_bits_per_sec > 0 ? { value: c.min_bits_per_sec, label: '최소 ' + fmtBits(c.min_bits_per_sec) } : null);
    charts.rtt.setThreshold(c.max_rtt_ms > 0 ? { value: c.max_rtt_ms, label: '최대 ' + fmtMs(c.max_rtt_ms) } : null);
    charts.jit.setThreshold(c.max_jitter_ms > 0 ? { value: c.max_jitter_ms, label: '최대 ' + fmtMs(c.max_jitter_ms, 3) } : null);
    charts.loss.setThreshold(udp && c.check_loss && c.max_loss_pct > 0 ? { value: c.max_loss_pct, label: '최대 ' + fmtPct(c.max_loss_pct) } : null);
    $('summaryBox').hidden = true;
    $('verdict').hidden = true;
    $('intervalTable').querySelector('tbody').innerHTML = '';
    buildTableHeads(udp);
    renderStreams();
    renderKpis();
    setProgress(0);
  }

  function buildTableHeads(udp) {
    $('streamTable').querySelector('thead').innerHTML = udp
      ? '<tr><th>스트림</th><th>현재 대역폭</th><th>평균 대역폭</th><th>수신량</th><th>수신 패킷</th><th>손실</th><th>손실률</th><th>지터</th><th>순서 뒤바뀜</th></tr>'
      : '<tr><th>스트림</th><th>현재 대역폭</th><th>평균 대역폭</th><th>수신량</th></tr>';
    $('intervalTable').querySelector('thead').innerHTML = udp
      ? '<tr><th>구간</th><th>전송량</th><th>대역폭</th><th>지터</th><th>손실/전체</th><th>손실률</th><th>RTT 평균</th><th>RTT 최대</th></tr>'
      : '<tr><th>구간</th><th>전송량</th><th>대역폭</th><th>RTT 평균</th><th>RTT 최대</th><th>RTT 지터</th></tr>';
  }

  function fullIntervals() {
    // exclude a short trailing partial interval from min/max
    const ivs = view.intervals;
    if (ivs.length > 1) {
      const last = ivs[ivs.length - 1];
      if (last.end - last.start < 0.5 * (view.params.interval_sec || 1)) return ivs.slice(0, -1);
    }
    return ivs;
  }

  function addInterval(iv) {
    view.intervals.push(iv);
    const udp = view.params.protocol === 'udp';
    const lat = iv.latency;
    const hasLat = lat && lat.received > 0;
    charts.bw.push(iv.end, [iv.sum.bits_per_sec]);
    charts.rtt.push(iv.end, [hasLat ? lat.avg_ms : null, hasLat ? lat.max_ms : null]);
    charts.jit.push(iv.end, udp ? [iv.sum.jitter_ms || 0, hasLat ? lat.jitter_ms : null] : [hasLat ? lat.jitter_ms : null]);
    charts.loss.push(iv.end, [udp ? (iv.sum.loss_pct || 0) : (lat ? lat.loss_pct : null)]);
    appendIntervalRow(iv);
    renderStreams();
    renderKpis();
    if (view.live) setProgress(iv.end / view.params.duration_sec);
  }

  function appendIntervalRow(iv) {
    const udp = view.params.protocol === 'udp';
    const l = iv.latency && iv.latency.received > 0 ? iv.latency : null;
    const s = iv.sum;
    const tr = document.createElement('tr');
    tr.innerHTML = udp
      ? `<td>${iv.start.toFixed(1)}–${iv.end.toFixed(1)}s</td><td>${fmtBytes(s.bytes)}</td><td>${fmtBits(s.bits_per_sec, true)}</td><td>${fmtMs(s.jitter_ms || 0, 3)}</td><td>${fmtInt(s.lost || 0)}/${fmtInt((s.packets || 0) + (s.lost || 0))}</td><td>${fmtPct(s.loss_pct || 0)}</td><td>${l ? fmtMs(l.avg_ms) : '-'}</td><td>${l ? fmtMs(l.max_ms) : '-'}</td>`
      : `<td>${iv.start.toFixed(1)}–${iv.end.toFixed(1)}s</td><td>${fmtBytes(s.bytes)}</td><td>${fmtBits(s.bits_per_sec, true)}</td><td>${l ? fmtMs(l.avg_ms) : '-'}</td><td>${l ? fmtMs(l.max_ms) : '-'}</td><td>${l ? fmtMs(l.jitter_ms) : '-'}</td>`;
    $('intervalTable').querySelector('tbody').appendChild(tr);
  }

  function aggregate() {
    const ivs = view.intervals, full = fullIntervals();
    const udp = view.params.protocol === 'udp';
    const a = { cur: null, avg: null, min: null, max: null, rttCur: null, rttAvg: null, rttMax: null, jitCur: null, jitAvg: null, lossCur: null, lost: 0, total: 0 };
    if (!ivs.length) return a;
    const last = ivs[ivs.length - 1];
    let bytes = 0, dur = 0;
    ivs.forEach(iv => { bytes += iv.sum.bytes; dur += iv.end - iv.start; });
    a.avg = dur > 0 ? bytes * 8 / dur : 0;
    const bps = full.map(iv => iv.sum.bits_per_sec);
    a.min = Math.min(...bps); a.max = Math.max(...bps);
    a.cur = (full[full.length - 1] || last).sum.bits_per_sec;
    let rs = 0, rn = 0;
    ivs.forEach(iv => {
      const l = iv.latency;
      if (l && l.received > 0) { rs += l.avg_ms * l.received; rn += l.received; a.rttMax = Math.max(a.rttMax ?? 0, l.max_ms); a.rttCur = l.avg_ms; }
    });
    a.rttAvg = rn ? rs / rn : null;
    if (udp) {
      const js = ivs.filter(iv => iv.sum.packets > 0).map(iv => iv.sum.jitter_ms || 0);
      a.jitCur = js.length ? js[js.length - 1] : 0;
      a.jitAvg = js.length ? js.reduce((x, y) => x + y, 0) / js.length : 0;
      ivs.forEach(iv => { a.lost += iv.sum.lost || 0; a.total += (iv.sum.packets || 0) + (iv.sum.lost || 0); });
      a.lossCur = a.total ? a.lost * 100 / a.total : 0;
    } else {
      const js = ivs.filter(iv => iv.latency && iv.latency.received > 1).map(iv => iv.latency.jitter_ms);
      a.jitCur = js.length ? js[js.length - 1] : null;
      a.jitAvg = js.length ? js.reduce((x, y) => x + y, 0) / js.length : null;
      ivs.forEach(iv => { if (iv.latency) { a.total += iv.latency.sent; a.lost += Math.max(0, iv.latency.sent - iv.latency.received); } });
      a.lossCur = a.total ? a.lost * 100 / a.total : null;
    }
    return a;
  }

  function kpi(id, val, sub, state) {
    const el = $(id);
    el.querySelector('.k-val').textContent = val;
    el.querySelector('.k-sub').textContent = sub;
    el.classList.toggle('good', state === true);
    el.classList.toggle('bad', state === false);
  }

  function renderKpis() {
    if (!view) return;
    const udp = view.params.protocol === 'udp';
    const c = view.criteria;
    const done = view.record && view.record.result;
    const S = done ? view.record.result.summary : null;
    const a = aggregate();
    const judge = (enabled, ok) => enabled ? ok : undefined;

    const bwMain = done ? S.bits_per_sec : a.cur;
    kpi('kBw', fmtBits(bwMain, true),
      `평균 ${fmtBits(done ? S.bits_per_sec : a.avg)} · 최소 ${fmtBits(done ? S.min_bits_per_sec : a.min)} · 최대 ${fmtBits(done ? S.max_bits_per_sec : a.max)}`,
      bwMain != null ? judge(c.min_bits_per_sec > 0, (done ? S.bits_per_sec : a.avg) >= c.min_bits_per_sec) : undefined);

    const L = done ? S.latency : null;
    const rttMain = done ? (L && L.received ? L.avg_ms : null) : a.rttCur;
    kpi('kRtt', fmtMs(rttMain),
      done && L && L.received ? `최소 ${fmtMs(L.min_ms)} · p95 ${fmtMs(L.p95_ms)} · 최대 ${fmtMs(L.max_ms)}` : `평균 ${fmtMs(a.rttAvg)} · 최대 ${fmtMs(a.rttMax)}`,
      rttMain != null ? judge(c.max_rtt_ms > 0, (done ? rttMain : a.rttAvg) <= c.max_rtt_ms) : undefined);

    let jit = done ? (udp ? S.jitter_ms : (L ? L.jitter_ms : null)) : a.jitCur;
    kpi('kJit', fmtMs(jit, 3), (udp ? 'UDP 도착 간격 지터' : 'RTT 변동') + (done ? '' : ` · 평균 ${fmtMs(a.jitAvg, 3)}`),
      jit != null ? judge(c.max_jitter_ms > 0, (done ? jit : a.jitAvg) <= c.max_jitter_ms) : undefined);

    if (udp) {
      const lossPct = done ? S.loss_pct || 0 : a.lossCur;
      const lost = done ? S.lost || 0 : a.lost;
      const total = done ? S.packets_sent || 0 : a.total;
      kpi('kLoss', fmtPct(lossPct), `${fmtInt(lost)} / ${fmtInt(total)} 패킷` + (done && S.out_of_order ? ` · 순서 뒤바뀜 ${fmtInt(S.out_of_order)}` : ''),
        lossPct != null ? judge(c.check_loss, lossPct <= c.max_loss_pct) : undefined);
    } else {
      const lp = done ? (L ? L.loss_pct : null) : a.lossCur;
      kpi('kLoss', lp == null ? '-' : fmtPct(lp), 'TCP: RTT 프로브 손실률', undefined);
    }
  }

  function renderStreams() {
    const tb = $('streamTable').querySelector('tbody');
    if (!view || !view.intervals.length) { tb.innerHTML = '<tr><td colspan="9" class="muted">데이터 없음</td></tr>'; return; }
    const udp = view.params.protocol === 'udp';
    const ivs = view.intervals, full = fullIntervals();
    const lastFull = full[full.length - 1];
    const n = ivs[0].streams.length;
    const dur = ivs.reduce((s, iv) => s + iv.end - iv.start, 0);
    const final = view.record && view.record.result ? view.record.result.summary.streams : null;
    const rows = [];
    const tot = { bytes: 0, packets: 0, lost: 0, ooo: 0, cur: 0 };
    for (let i = 0; i < n; i++) {
      let bytes = 0, packets = 0, lost = 0, ooo = 0, js = [];
      ivs.forEach(iv => { const s = iv.streams[i]; if (!s) return; bytes += s.bytes; packets += s.packets || 0; lost += s.lost || 0; ooo += s.out_of_order || 0; if (s.packets) js.push(s.jitter_ms || 0); });
      if (final && final[i]) { bytes = final[i].bytes; if (udp) { packets = final[i].packets || 0; lost = final[i].lost || 0; } }
      const cur = lastFull.streams[i] ? lastFull.streams[i].bits_per_sec : 0;
      const avg = dur > 0 ? bytes * 8 / dur : 0;
      const jit = js.length ? js.reduce((a, b) => a + b, 0) / js.length : 0;
      tot.bytes += bytes; tot.packets += packets; tot.lost += lost; tot.ooo += ooo; tot.cur += cur;
      rows.push(udp
        ? `<tr><td>#${i + 1}</td><td>${fmtBits(cur, true)}</td><td>${fmtBits(avg, true)}</td><td>${fmtBytes(bytes)}</td><td>${fmtInt(packets)}</td><td>${fmtInt(lost)}</td><td>${fmtPct(packets + lost ? lost * 100 / (packets + lost) : 0)}</td><td>${fmtMs(jit, 3)}</td><td>${fmtInt(ooo)}</td></tr>`
        : `<tr><td>#${i + 1}</td><td>${fmtBits(cur, true)}</td><td>${fmtBits(avg, true)}</td><td>${fmtBytes(bytes)}</td></tr>`);
    }
    if (n > 1) {
      const avg = dur > 0 ? tot.bytes * 8 / dur : 0;
      rows.push(udp
        ? `<tr class="sum"><td>합계</td><td>${fmtBits(tot.cur, true)}</td><td>${fmtBits(avg, true)}</td><td>${fmtBytes(tot.bytes)}</td><td>${fmtInt(tot.packets)}</td><td>${fmtInt(tot.lost)}</td><td>${fmtPct(tot.packets + tot.lost ? tot.lost * 100 / (tot.packets + tot.lost) : 0)}</td><td></td><td>${fmtInt(tot.ooo)}</td></tr>`
        : `<tr class="sum"><td>합계</td><td>${fmtBits(tot.cur, true)}</td><td>${fmtBits(avg, true)}</td><td>${fmtBytes(tot.bytes)}</td></tr>`);
    }
    tb.innerHTML = rows.join('');
  }

  function setProgress(f) { $('progressBar').style.width = Math.max(0, Math.min(1, f)) * 100 + '%'; }

  const dirLabel = p => p.reverse ? '상대 → 이 PC' : '이 PC → 상대';
  const describe = p => `${p.protocol.toUpperCase()} · ${dirLabel(p)} · 스트림 ${p.streams} · ${p.duration_sec}초` + (p.bandwidth ? ` · 목표 ${fmtBits(p.bandwidth)}` : '');

  const CHECK_NAMES = { bandwidth: '평균 대역폭', loss: '패킷 손실률', rtt: '평균 RTT', jitter: '지터' };

  function finishView(rec) {
    view.record = rec; view.live = false;
    stopLiveTimer();
    setProgress(1);
    const r = rec.result, s = r.summary, udp = r.params.protocol === 'udp';
    $('stateText').textContent = rec.stopped ? '중지됨 (부분 결과)' : '테스트 완료';
    $('stateSub').textContent = `${rec.target} · ${describe(r.params)} · ${fmtTime(rec.created_at)}`;
    const v = rec.verdict, vd = $('verdict');
    vd.hidden = false;
    vd.className = 'verdict ' + (!v.evaluated ? 'none' : v.pass ? 'pass' : 'fail');
    vd.textContent = !v.evaluated ? '판정 기준 없음' : v.pass ? 'PASS' : 'FAIL';

    const items = [
      ['평균 대역폭', fmtBits(s.bits_per_sec, true)],
      ['최소 / 최대', `${fmtBits(s.min_bits_per_sec)} / ${fmtBits(s.max_bits_per_sec)}`],
      ['수신량 (송신량)', `${fmtBytes(s.bytes)} (${fmtBytes(s.sent_bytes)})`],
      ['측정 시간', s.duration_sec.toFixed(2) + ' 초'],
    ];
    if (udp) items.push(
      ['패킷 손실', `${fmtPct(s.loss_pct || 0)} (${fmtInt(s.lost || 0)}/${fmtInt(s.packets_sent || 0)})`],
      ['UDP 지터', fmtMs(s.jitter_ms || 0, 3)],
      ['순서 뒤바뀜', fmtInt(s.out_of_order || 0)]);
    const L = s.latency;
    if (L && L.received) items.push(
      ['RTT 평균 / p95', `${fmtMs(L.avg_ms)} / ${fmtMs(L.p95_ms)}`],
      ['RTT 최소 / 최대', `${fmtMs(L.min_ms)} / ${fmtMs(L.max_ms)}`],
      ['RTT 지터', fmtMs(L.jitter_ms)],
      ['프로브 손실', `${fmtPct(L.loss_pct)} (${L.sent - L.received}/${L.sent})`]);
    $('summaryGrid').innerHTML = items.map(([k, val]) => `<div><span>${k}</span><b>${val}</b></div>`).join('');
    $('summaryMeta').textContent = r.client_host && r.server_host ? `${r.client_host} ↔ ${r.server_host}` : '';
    $('checks').innerHTML = v.evaluated
      ? `<table><thead><tr><th>판정 항목</th><th>기준</th><th>측정값</th><th>결과</th></tr></thead><tbody>${v.checks.map(c =>
        `<tr><td>${CHECK_NAMES[c.name] || c.name}</td><td>${esc(c.limit)}</td><td>${esc(c.measured)}</td><td><span class="tag ${c.pass ? 'pass' : 'fail'}">${c.pass ? 'PASS' : 'FAIL'}</span></td></tr>`).join('')}</tbody></table>`
      : '';
    $('dlJson').onclick = () => { location.href = `/api/results/${rec.id}?download=1`; };
    $('summaryBox').hidden = false;
    renderStreams();
    renderKpis();
  }

  function startLiveTimer() {
    stopLiveTimer();
    liveTimer = setInterval(() => {
      if (!view || !view.live) return;
      const el = (Date.now() - view.startedAt.getTime()) / 1000;
      const total = view.params.duration_sec;
      setProgress(el / total);
      const rem = Math.max(0, total - el);
      $('stateSub').textContent = `${view.target} · ${describe(view.params)} · 남은 시간 ${rem.toFixed(0)}초`;
    }, 250);
  }
  function stopLiveTimer() { clearInterval(liveTimer); liveTimer = null; }

  function setRunning(running) {
    const b = $('startBtn');
    b.textContent = running ? '테스트 중지' : '테스트 시작';
    b.className = 'btn big ' + (running ? 'danger' : 'primary');
    b.dataset.running = running ? '1' : '';
  }

  function beginLive(lt) {
    resetView(lt.id, lt.params, lt.criteria, lt.started_at, true);
    view.target = lt.target;
    $('stateText').textContent = '테스트 진행 중';
    (lt.intervals || []).forEach(addInterval);
    setRunning(true);
    startLiveTimer();
  }

  // ---------- actions ----------
  $('startBtn').onclick = async () => {
    $('formErr').textContent = '';
    if ($('startBtn').dataset.running) {
      try { await api('/api/test/stop', {}); } catch (e) { toast(e.message); }
      return;
    }
    const req = readForm();
    if (!req.host) { $('formErr').textContent = '상대 PC 주소를 입력하세요.'; $('host').focus(); return; }
    saveForm();
    $('startBtn').disabled = true;
    try {
      const lt = await api('/api/test/start', req);
      if (!view || view.id !== lt.id) beginLive(lt);
    } catch (e) {
      $('formErr').textContent = e.message;
    } finally {
      $('startBtn').disabled = false;
    }
  };

  $('srvToggle').onclick = async () => {
    try {
      if (state.server && state.server.running) await api('/api/server/stop', {});
      else await api('/api/server/start', { port: state.server ? state.server.port : 5210 });
    } catch (e) { toast('서버: ' + e.message); }
    refreshStatus();
  };

  // ---------- server status / history ----------
  const state = { server: null };
  function renderServer(s) {
    state.server = s;
    $('srvDot').className = 'dot ' + (s.running ? 'on' : 'off');
    $('srvText').textContent = s.running ? `테스트 서버 대기 중 (포트 ${s.port})` : (s.error ? `서버 꺼짐: ${s.error}` : '테스트 서버 꺼짐');
    $('srvToggle').textContent = s.running ? '서버 끄기' : '서버 켜기';
  }

  async function refreshStatus() {
    try {
      const st = await api('/api/status');
      $('hostname').textContent = st.hostname;
      $('version').textContent = 'v' + st.version;
      $('addrs').textContent = (st.addresses || []).join('  ·  ');
      $('resultsDir').textContent = '저장 위치: ' + st.results_dir;
      renderServer(st.server);
      if (st.live && (!view || view.id !== st.live.id)) beginLive(st.live);
      if (!st.live && view && view.live) { setRunning(false); }
    } catch (e) { /* server not reachable */ }
  }

  async function refreshHistory() {
    let list = [];
    try { list = await api('/api/results'); } catch { }
    const tb = $('historyTable').querySelector('tbody');
    if (!list.length) { tb.innerHTML = '<tr><td colspan="8" class="muted">아직 저장된 결과가 없습니다.</td></tr>'; return; }
    tb.innerHTML = list.map(r => {
      const tag = !r.evaluated ? '<span class="tag none">-</span>' : r.pass ? '<span class="tag pass">PASS</span>' : '<span class="tag fail">FAIL</span>';
      return `<tr data-id="${esc(r.id)}"><td>${fmtTime(r.created_at)}${r.stopped ? ' <span class="muted small">(중지)</span>' : ''}</td><td>${esc(r.target)}</td>` +
        `<td>${r.protocol.toUpperCase()} ${r.reverse ? '↓' : '↑'} ×${r.streams} · ${r.duration_sec.toFixed(0)}s</td>` +
        `<td>${fmtBits(r.bits_per_sec, true)}</td><td>${r.protocol === 'udp' ? fmtPct(r.loss_pct || 0) : '-'}</td><td>${r.rtt_ms ? fmtMs(r.rtt_ms) : '-'}</td>` +
        `<td>${tag}</td><td><button class="icon-btn" data-del="${esc(r.id)}" title="삭제">✕</button></td></tr>`;
    }).join('');
  }

  $('historyTable').addEventListener('click', async e => {
    const del = e.target.closest('[data-del]');
    if (del) {
      e.stopPropagation();
      if (del.dataset.armed) {
        try { await api('/api/results/' + del.dataset.del, undefined, 'DELETE'); } catch (err) { toast(err.message); }
        refreshHistory();
      } else {
        del.dataset.armed = '1'; del.textContent = '삭제?';
        setTimeout(() => { if (del.isConnected) { delete del.dataset.armed; del.textContent = '✕'; } }, 3000);
      }
      return;
    }
    const tr = e.target.closest('tr[data-id]');
    if (!tr) return;
    if (view && view.live) { toast('테스트 진행 중에는 이력을 열 수 없습니다.'); return; }
    try {
      const rec = await api('/api/results/' + tr.dataset.id);
      showRecord(rec);
      window.scrollTo({ top: 0, behavior: 'smooth' });
    } catch (err) { toast(err.message); }
  });

  function showRecord(rec) {
    const r = rec.result;
    resetView(rec.id, r.params, rec.criteria, rec.created_at, false);
    r.intervals.forEach(addInterval);
    finishView(rec);
  }

  // ---------- events ----------
  function connectEvents() {
    const es = new EventSource('/api/events');
    const on = (name, fn) => es.addEventListener(name, e => { try { fn(JSON.parse(e.data)); } catch (err) { console.error(err); } });
    on('server', renderServer);
    on('log', m => {
      const el = $('log');
      el.textContent += `[${new Date(m.time).toLocaleTimeString('ko-KR', { hour12: false })}] ${m.message}\n`;
      el.scrollTop = el.scrollHeight;
    });
    on('test_start', lt => { if (!view || view.id !== lt.id) beginLive(lt); });
    on('interval', m => { if (view && view.id === m.id && view.live) addInterval(m.interval); });
    on('test_done', rec => {
      setRunning(false);
      if (view && view.id === rec.id) finishView(rec);
      refreshHistory();
    });
    on('test_error', m => {
      setRunning(false);
      if (view && view.id === m.id) {
        view.live = false; stopLiveTimer();
        $('stateText').textContent = '테스트 실패';
        $('stateSub').textContent = m.error;
      }
      $('formErr').textContent = m.error;
    });
    es.onopen = () => refreshStatus();
  }

  // ---------- init ----------
  loadForm();
  updateFormHints();
  buildTableHeads(seg('proto') === 'udp');
  renderStreams();
  refreshStatus();
  refreshHistory();
  connectEvents();
})();

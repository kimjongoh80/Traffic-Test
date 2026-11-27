// Minimal dependency-free canvas line chart (works offline).
(function () {
  function cssVar(name) {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  }
  function niceStep(range, ticks) {
    const raw = range / ticks;
    const mag = Math.pow(10, Math.floor(Math.log10(raw)));
    const n = raw / mag;
    return (n < 1.5 ? 1 : n < 3 ? 2 : n < 7 ? 5 : 10) * mag;
  }

  class LineChart {
    // opts: { series: [{name, color: '--s1', dash}], yFormat: fn, threshold: {value, label}, yMin }
    constructor(canvas, opts) {
      this.c = canvas;
      this.ctx = canvas.getContext('2d');
      this.opts = Object.assign({ yFormat: v => String(v), series: [] }, opts);
      this.xs = [];
      this.ys = this.opts.series.map(() => []);
      this.hover = -1;
      this.tip = document.createElement('div');
      this.tip.className = 'chart-tip';
      this.tip.hidden = true;
      canvas.parentElement.appendChild(this.tip);
      new ResizeObserver(() => this.draw()).observe(canvas);
      canvas.addEventListener('mousemove', e => this.onMove(e));
      canvas.addEventListener('mouseleave', () => { this.hover = -1; this.tip.hidden = true; this.draw(); });
      matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => this.draw());
    }
    setSeries(series) { this.opts.series = series; this.ys = series.map(() => []); this.xs = []; this.draw(); }
    setThreshold(t) { this.opts.threshold = t; this.draw(); }
    clear() { this.xs = []; this.ys = this.opts.series.map(() => []); this.draw(); }
    push(x, values) {
      this.xs.push(x);
      values.forEach((v, i) => this.ys[i] && this.ys[i].push(v));
      this.draw();
    }
    layout() {
      const w = this.c.clientWidth, h = this.c.clientHeight;
      // Page zoom (UI size) changes the on-screen size; render at that resolution.
      const rw = this.c.getBoundingClientRect().width;
      const dpr = (window.devicePixelRatio || 1) * (w > 0 && rw > 0 ? rw / w : 1);
      if (this.c.width !== Math.round(w * dpr) || this.c.height !== Math.round(h * dpr)) {
        this.c.width = Math.round(w * dpr);
        this.c.height = Math.round(h * dpr);
      }
      this.ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      return { w, h, l: 76, r: 12, t: 28, b: 28 };
    }
    yRange() {
      let max = 0, min = this.opts.yMin ?? 0;
      this.ys.forEach(s => s.forEach(v => { if (v != null && isFinite(v)) max = Math.max(max, v); }));
      const th = this.opts.threshold;
      if (th && th.value > 0) max = Math.max(max, th.value * 1.1);
      if (max <= min) max = min + 1;
      const step = niceStep(max - min, 4);
      return { min, max: Math.ceil(max / step) * step, step };
    }
    draw() {
      const ctx = this.ctx, L = this.layout();
      const pw = L.w - L.l - L.r, ph = L.h - L.t - L.b;
      ctx.clearRect(0, 0, L.w, L.h);
      if (pw <= 0 || ph <= 0) return;
      const grid = cssVar('--grid'), muted = cssVar('--muted');
      const yr = this.yRange();
      const xmax = Math.max(this.xs.length ? this.xs[this.xs.length - 1] : 0, this.opts.xMax || 0, 1);
      const X = x => L.l + (x / xmax) * pw;
      const Y = y => L.t + ph - ((y - yr.min) / (yr.max - yr.min)) * ph;
      this._geom = { L, X, xmax, pw };

      ctx.font = '14px system-ui, sans-serif';
      ctx.lineWidth = 1;
      ctx.textBaseline = 'middle';
      for (let v = yr.min; v <= yr.max + 1e-9; v += yr.step) {
        const y = Math.round(Y(v)) + 0.5;
        ctx.strokeStyle = grid; ctx.beginPath(); ctx.moveTo(L.l, y); ctx.lineTo(L.w - L.r, y); ctx.stroke();
        ctx.fillStyle = muted; ctx.textAlign = 'right'; ctx.fillText(this.opts.yFormat(v), L.l - 6, y);
      }
      const xstep = Math.max(1, niceStep(xmax, 6) || 1);
      ctx.textAlign = 'center'; ctx.textBaseline = 'top';
      for (let x = 0; x <= xmax + 1e-9; x += xstep) {
        ctx.fillStyle = muted; ctx.fillText(Math.round(x) + 's', X(x), L.t + ph + 6);
      }

      const th = this.opts.threshold;
      if (th && th.value > 0) {
        const y = Y(th.value);
        ctx.strokeStyle = cssVar('--bad'); ctx.setLineDash([5, 4]); ctx.lineWidth = 1.2;
        ctx.beginPath(); ctx.moveTo(L.l, y); ctx.lineTo(L.w - L.r, y); ctx.stroke(); ctx.setLineDash([]);
        ctx.fillStyle = cssVar('--bad'); ctx.textAlign = 'right'; ctx.textBaseline = 'bottom';
        ctx.fillText(th.label || '기준', L.w - L.r - 2, y - 2);
      }

      this.opts.series.forEach((s, i) => {
        const ys = this.ys[i];
        if (!ys.length) return;
        ctx.strokeStyle = cssVar(s.color); ctx.lineWidth = s.width || 2;
        ctx.setLineDash(s.dash || []);
        ctx.beginPath();
        let started = false;
        ys.forEach((v, j) => {
          if (v == null || !isFinite(v)) { started = false; return; }
          const px = X(this.xs[j]), py = Y(v);
          if (!started) { ctx.moveTo(px, py); started = true; } else ctx.lineTo(px, py);
        });
        ctx.stroke(); ctx.setLineDash([]);
        if (ys.length < 40) {
          ctx.fillStyle = cssVar(s.color);
          ys.forEach((v, j) => { if (v != null && isFinite(v)) { ctx.beginPath(); ctx.arc(X(this.xs[j]), Y(v), 2.2, 0, 7); ctx.fill(); } });
        }
      });

      // legend
      ctx.textAlign = 'left'; ctx.textBaseline = 'middle';
      let lx = L.l;
      this.opts.series.forEach(s => {
        ctx.fillStyle = cssVar(s.color); ctx.fillRect(lx, 9, 12, 3);
        ctx.fillStyle = muted; ctx.fillText(s.name, lx + 16, 11);
        lx += 26 + ctx.measureText(s.name).width;
      });

      if (this.hover >= 0 && this.hover < this.xs.length) {
        const x = Math.round(X(this.xs[this.hover])) + 0.5;
        ctx.strokeStyle = muted; ctx.beginPath(); ctx.moveTo(x, L.t); ctx.lineTo(x, L.t + ph); ctx.stroke();
      }
    }
    onMove(e) {
      if (!this.xs.length || !this._geom) return;
      const rect = this.c.getBoundingClientRect();
      const mx = (e.clientX - rect.left) * (rect.width > 0 ? this.c.clientWidth / rect.width : 1);
      const { X } = this._geom;
      let best = 0, bd = Infinity;
      this.xs.forEach((x, i) => { const d = Math.abs(X(x) - mx); if (d < bd) { bd = d; best = i; } });
      this.hover = best;
      this.draw();
      const rows = this.opts.series.map((s, i) => {
        const v = this.ys[i][best];
        return `<div><span class="sw" style="background:${cssVar(s.color)}"></span>${s.name}: <b>${v == null ? '-' : this.opts.yFormat(v, true)}</b></div>`;
      }).join('');
      this.tip.innerHTML = `<div class="muted">${this.xs[best].toFixed(1)}s</div>${rows}`;
      this.tip.hidden = false;
      const px = X(this.xs[best]);
      const left = px + 14 + this.tip.offsetWidth > this.c.clientWidth ? px - this.tip.offsetWidth - 6 : px + 20;
      this.tip.style.left = left + 'px';
      this.tip.style.top = '30px';
    }
  }
  window.LineChart = LineChart;
})();

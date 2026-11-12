import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, computed, inject, input, output, signal } from '@angular/core';

export interface Series {
  key: string;
  name: string;
  color: string;
}
export interface BarDatum {
  label: string;
  title: string;
  values: Record<string, number>;
}

/**
 * Stacked vertical bars. Marks follow the dataviz spec: 2px surface gap between
 * segments, 4px rounded data-end, recessive grid, per-bar hover tooltip with a
 * hit target wider than the mark.
 */
@Component({
  selector: 'cz-stacked-bars',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block relative' },
  template: `
    <svg [attr.viewBox]="'0 0 ' + W + ' ' + height()" class="block w-full h-auto overflow-visible" role="img" [attr.aria-label]="ariaLabel()">
      @for (g of grid(); track g.v) {
        <line [attr.x1]="L" [attr.x2]="W - R" [attr.y1]="g.y" [attr.y2]="g.y" stroke="var(--grid)" />
        <text [attr.x]="L - 6" [attr.y]="g.y + 4" font-size="10" text-anchor="end" fill="var(--muted-foreground)">{{ yFormat()(g.v) }}</text>
      }
      @for (b of bars(); track $index) {
        @for (s of b.segs; track s.key) {
          @if (s.top) {
            <path [attr.d]="s.d" [attr.fill]="s.color" />
          } @else {
            <rect [attr.x]="b.x" [attr.y]="s.y" [attr.width]="bw()" [attr.height]="s.h" [attr.fill]="s.color" />
          }
        }
        @if (b.showLabel) {
          <text [attr.x]="b.x + bw() / 2" [attr.y]="height() - 7" font-size="10" text-anchor="middle" fill="var(--muted-foreground)">{{ b.label }}</text>
        }
        <rect [attr.x]="b.hx" [attr.y]="T" [attr.width]="b.hw" [attr.height]="height() - T - B" fill="transparent"
          [style.cursor]="clickable() ? 'pointer' : 'default'"
          (mouseenter)="hover.set($index)" (mouseleave)="hover.set(null)" (click)="pick.emit($index)" />
      }
      @if (hover() != null) {
        <rect [attr.x]="bars()[hover()!].hx" [attr.y]="T" [attr.width]="bars()[hover()!].hw" [attr.height]="height() - T - B" fill="var(--foreground)" opacity=".05" pointer-events="none" />
      }
    </svg>
    @if (tip(); as t) {
      <div class="chart-tip" [style.left.%]="t.left" [style.top.px]="8" [style.transform]="t.flip ? 'translateX(-100%)' : 'translateX(12px)'">
        <b>{{ t.title }}</b>
        @for (r of t.rows; track r.name) {
          <span class="r"><i [style.background]="r.color"></i><span>{{ r.name }}</span><span class="text-right">{{ r.value }}</span></span>
        }
      </div>
    }
  `,
})
export class StackedBars {
  readonly data = input.required<BarDatum[]>();
  readonly series = input.required<Series[]>();
  readonly height = input(210);
  readonly yFormat = input<(v: number) => string>((v) => String(v));
  readonly valueFormat = input<(v: number) => string>((v) => String(v));
  readonly ariaLabel = input('');
  readonly clickable = input(false);
  readonly pick = output<number>();
  readonly hover = signal<number | null>(null);

  /** Drawing width follows the element width so labels render at their real size. */
  readonly w = signal(640);
  get W(): number {
    return this.w();
  }

  constructor() {
    const el = inject(ElementRef<HTMLElement>).nativeElement as HTMLElement;
    const ro = new ResizeObserver(([e]) => {
      const cw = Math.round(e.contentRect.width);
      if (cw > 0 && cw !== this.w()) this.w.set(cw);
    });
    ro.observe(el);
    inject(DestroyRef).onDestroy(() => ro.disconnect());
  }
  readonly L = 46;
  readonly R = 8;
  readonly T = 10;
  readonly B = 24;

  private readonly max = computed(() => {
    let mx = 0;
    for (const d of this.data()) {
      let t = 0;
      for (const s of this.series()) t += d.values[s.key] ?? 0;
      mx = Math.max(mx, t);
    }
    return niceMax(mx);
  });

  readonly grid = computed(() => {
    const mx = this.max();
    return [0, 1, 2, 3, 4].map((k) => ({ v: (mx / 4) * k, y: this.y((mx / 4) * k) }));
  });

  readonly bw = computed(() => {
    const n = Math.max(this.data().length, 1);
    const slot = (this.W - this.L - this.R) / n;
    return Math.max(Math.min(slot * 0.6, 56), Math.min(2, slot));
  });

  private y(v: number): number {
    const h = this.height() - this.T - this.B;
    return this.T + h * (1 - v / (this.max() || 1));
  }

  readonly bars = computed(() => {
    const n = Math.max(this.data().length, 1);
    const slot = (this.W - this.L - this.R) / n;
    const bw = this.bw();
    const every = Math.ceil(n / 12);
    return this.data().map((d, i) => {
      const cx = this.L + slot * (i + 0.5);
      const x = cx - bw / 2;
      const present = this.series().filter((s) => (d.values[s.key] ?? 0) > 0);
      let acc = 0;
      const segs = present.map((s, k) => {
        const v = d.values[s.key] ?? 0;
        const y0 = this.y(acc);
        const y1 = this.y(acc + v);
        acc += v;
        const top = k === present.length - 1;
        const gap = top || bw < 4 ? 0 : 2;
        const h = Math.max(y0 - y1 - gap, 0.5);
        const r = Math.min(4, bw / 2, h);
        const d2 = `M${x},${y0} V${y1 + r} Q${x},${y1} ${x + r},${y1} H${x + bw - r} Q${x + bw},${y1} ${x + bw},${y1 + r} V${y0} Z`;
        return { key: s.key, color: s.color, y: y0 - h, h, top, d: d2 };
      });
      return { x, label: d.label, showLabel: i % every === 0 || i === n - 1, hx: this.L + slot * i, hw: slot, segs };
    });
  });

  readonly tip = computed(() => {
    const i = this.hover();
    if (i == null) return null;
    const d = this.data()[i];
    const fmt = this.valueFormat();
    let t = 0;
    const rows = this.series()
      .filter((s) => (d.values[s.key] ?? 0) > 0)
      .map((s) => {
        t += d.values[s.key];
        return { name: s.name, color: s.color, value: fmt(d.values[s.key]) };
      })
      .reverse();
    const b = this.bars()[i];
    const left = ((b.hx + b.hw) / this.W) * 100;
    return { title: `${d.title} · ${fmt(t)}`, rows, left, flip: left > 70 };
  });
}

export function niceMax(v: number): number {
  if (v <= 0) return 1;
  const p = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10]) if (m * p >= v) return m * p;
  return 10 * p;
}

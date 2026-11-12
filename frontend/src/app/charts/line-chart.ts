import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, computed, inject, input, output, signal } from '@angular/core';
import { niceMax } from './stacked-bars';

export interface LinePoint {
  label: string;
  title: string;
  value: number;
}

/**
 * One-series line with soft area, an optional dashed reference line (threshold
 * or plan fee) and an optional dashed projection. Crosshair + tooltip on hover.
 */
@Component({
  selector: 'cz-line-chart',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block relative' },
  template: `
    <svg [attr.viewBox]="'0 0 ' + W + ' ' + height()" class="block w-full h-auto overflow-visible" role="img" [attr.aria-label]="ariaLabel()"
      (mouseleave)="hover.set(null)">
      @for (g of grid(); track g.v) {
        <line [attr.x1]="L" [attr.x2]="W - R" [attr.y1]="g.y" [attr.y2]="g.y" stroke="var(--grid)" />
        <text [attr.x]="L - 6" [attr.y]="g.y + 4" font-size="10" text-anchor="end" fill="var(--muted-foreground)">{{ yFormat()(g.v) }}</text>
      }
      @if (refLine(); as r) {
        <line [attr.x1]="L" [attr.x2]="W - R" [attr.y1]="r.y" [attr.y2]="r.y" [attr.stroke]="refColor()" stroke-width="1.5" stroke-dasharray="5 4" />
        <text [attr.x]="W - R" [attr.y]="r.y - 5" font-size="10.5" text-anchor="end" [attr.fill]="refColor()">{{ refLabel() }}</text>
      }
      @if (pts().length) {
        <polygon [attr.points]="area()" [attr.fill]="color()" opacity=".12" />
        <polyline [attr.points]="line()" fill="none" [attr.stroke]="color()" stroke-width="2" stroke-linejoin="round" />
        @if (proj(); as p) {
          <line [attr.x1]="p.x1" [attr.y1]="p.y1" [attr.x2]="p.x2" [attr.y2]="p.y2" [attr.stroke]="color()" stroke-width="2" stroke-dasharray="4 4" />
          <text [attr.x]="p.x2 - 4" [attr.y]="p.y2 - 8" font-size="10.5" text-anchor="end" fill="var(--muted-foreground)">{{ projectionLabel() }}</text>
        }
        @if (pts().length <= 40) {
          @for (p of pts(); track $index) {
            <circle [attr.cx]="p.x" [attr.cy]="p.y" r="4" [attr.fill]="color()" stroke="var(--background)" stroke-width="2" />
          }
        }
      }
      @for (p of pts(); track $index) {
        @if (p.showLabel) {
          <text [attr.x]="p.x" [attr.y]="height() - 6" font-size="10" text-anchor="middle" fill="var(--muted-foreground)">{{ p.label }}</text>
        }
        <rect [attr.x]="p.x - slot() / 2" [attr.y]="T" [attr.width]="slot()" [attr.height]="height() - T - B" fill="transparent"
          [style.cursor]="clickable() ? 'pointer' : 'default'" (mouseenter)="hover.set($index)" (click)="pick.emit($index)" />
      }
      @if (hover() != null && pts()[hover()!]; as p) {
        <line [attr.x1]="p.x" [attr.x2]="p.x" [attr.y1]="T" [attr.y2]="height() - B" stroke="var(--muted-foreground)" stroke-dasharray="2 3" pointer-events="none" />
        <circle [attr.cx]="p.x" [attr.cy]="p.y" r="5" [attr.fill]="color()" stroke="var(--background)" stroke-width="2" pointer-events="none" />
      }
    </svg>
    @if (hover() != null && pts()[hover()!]; as p) {
      <div class="chart-tip" [style.left.%]="(p.x / W) * 100" [style.top.px]="6" [style.transform]="p.x / W > 0.7 ? 'translateX(calc(-100% - 10px))' : 'translateX(10px)'">
        <b>{{ data()[hover()!].title }}</b>
        <span class="r"><i [style.background]="color()"></i><span>{{ seriesName() }}</span><span class="text-right">{{ valueFormat()(data()[hover()!].value) }}</span></span>
        @if (ref() != null) {
          <span class="r"><i style="background: var(--muted-foreground)"></i><span>{{ refLabel() }}</span><span></span></span>
        }
      </div>
    }
  `,
})
export class LineChart {
  readonly data = input.required<LinePoint[]>();
  readonly height = input(140);
  readonly color = input('var(--s1)');
  readonly seriesName = input('Value');
  readonly ref = input<number | null>(null);
  readonly refLabel = input('');
  readonly refColor = input('var(--crit)');
  /** Extend the line to this many points with a dashed projection to this value. */
  readonly projectTo = input<{ points: number; value: number } | null>(null);
  readonly projectionLabel = input('');
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
  readonly R = 10;
  readonly T = 14;
  readonly B = 22;

  private readonly n = computed(() => Math.max(this.projectTo()?.points ?? 0, this.data().length, 1));
  private readonly max = computed(() =>
    niceMax(Math.max(...this.data().map((d) => d.value), this.ref() ?? 0, this.projectTo()?.value ?? 0) * 1.05),
  );
  readonly slot = computed(() => (this.W - this.L - this.R) / this.n());
  private x(i: number): number {
    return this.L + this.slot() * (i + 0.5);
  }
  private y(v: number): number {
    return this.T + (this.height() - this.T - this.B) * (1 - v / this.max());
  }

  readonly grid = computed(() => {
    const mx = this.max();
    return [0, 1, 2, 3, 4].map((k) => ({ v: (mx / 4) * k, y: this.y((mx / 4) * k) }));
  });
  readonly pts = computed(() => {
    const every = Math.ceil(this.n() / 10);
    return this.data().map((d, i) => ({ x: this.x(i), y: this.y(d.value), label: d.label, showLabel: i % every === 0 }));
  });
  readonly line = computed(() => this.pts().map((p) => `${p.x},${p.y}`).join(' '));
  readonly area = computed(() => {
    const p = this.pts();
    return `${p[0].x},${this.y(0)} ${this.line()} ${p[p.length - 1].x},${this.y(0)}`;
  });
  readonly refLine = computed(() => (this.ref() == null ? null : { y: this.y(this.ref()!) }));
  readonly proj = computed(() => {
    const pj = this.projectTo();
    const p = this.pts();
    if (!pj || !p.length || pj.points <= p.length) return null;
    const last = p[p.length - 1];
    return { x1: last.x, y1: last.y, x2: this.x(pj.points - 1), y2: this.y(pj.value) };
  });
}

import { ChangeDetectionStrategy, Component, computed, inject, input, model } from '@angular/core';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmToggleGroupImports } from '@spartan-ng/helm/toggle-group';
import { ins, m } from '../lib/api';
import { AGENTS, AGENT_COLOR, TOKEN_TYPES, fmtT, fmtUSD } from '../lib/format';
import { AppState } from '../lib/state';

@Component({
  selector: 'cz-agent',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'agent-tag' },
  template: `<i class="agent-dot" [style.background]="color()" aria-hidden="true"></i>{{ name() }}`,
})
export class AgentTag {
  readonly agent = input.required<string>();
  readonly name = computed(() => AGENTS[this.agent()] ?? this.agent());
  readonly color = computed(() => AGENT_COLOR[this.agent()] ?? 'var(--muted-foreground)');
}

/** Severity → the theme's status tone (follows the OS palette and contrast settings). */
export const TONE: Record<string, string> = { high: 'sn-tone-danger', medium: 'sn-tone-warning', ok: 'sn-tone-success' };

/** A segmented control (single choice), drawn by the skin's toggle-group styles. */
@Component({
  selector: 'cz-segmented',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [HlmToggleGroupImports],
  template: `
    <div hlmToggleGroup type="single" size="sm" [nullable]="false" [value]="value()" (valueChange)="pick($event)" [attr.aria-label]="label()">
      @for (o of options(); track o.id) {
        <button hlmToggleGroupItem [value]="o.id" class="h-7 min-w-7 px-2.5 text-xs">{{ o.label }}</button>
      }
    </div>
  `,
})
export class Segmented<T extends string | number> {
  readonly options = input.required<readonly { id: T; label: string }[]>();
  readonly value = model.required<T>();
  readonly label = input('');
  pick(v: unknown): void {
    if (v != null && v !== '') this.value.set(v as T);
  }
}

@Component({
  selector: 'cz-kpi',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'grid gap-0.5 rounded-lg border px-2.5 py-2 min-w-0' },
  template: `
    <span class="text-[11px] text-muted-foreground">{{ label() }}</span>
    <b class="tabular text-[15px] font-semibold truncate">{{ value() }}</b>
    @if (hint()) {
      <small class="text-[11px] text-muted-foreground truncate">{{ hint() }}</small>
    }
  `,
})
export class Kpi {
  readonly label = input.required<string>();
  readonly value = input.required<string>();
  readonly hint = input('');
}

/** Cost share by token type: a thin stacked bar plus a legend with values. */
@Component({
  selector: 'cz-token-breakdown',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'grid gap-2' },
  template: `
    <div class="flex h-2.5 gap-0.5" role="img" aria-label="Cost share by token type">
      @for (p of parts(); track p.key) {
        @if (p.share > 0) {
          <i class="block h-full rounded-[3px] min-w-0.5" [style.flex]="p.share" [style.background]="p.color" [title]="p.name"></i>
        }
      }
    </div>
    <div class="grid gap-1 text-xs">
      @for (p of parts(); track p.key) {
        <div class="grid grid-cols-[10px_minmax(0,1fr)_auto_auto_auto] items-center gap-2 tabular">
          <span class="size-2.5 rounded-[3px]" [style.background]="p.color"></span>
          <span>{{ p.name }}</span>
          <span class="mono text-muted-foreground">{{ fmtT(p.tokens) }}</span>
          <span class="w-14 text-right">{{ fmtUSD(p.cost) }}</span>
          <span class="w-9 text-right text-muted-foreground">{{ (p.share * 100).toFixed(0) }}%</span>
        </div>
      }
    </div>
  `,
})
export class TokenBreakdown {
  readonly usage = input.required<m.Usage>();
  readonly hideCacheWrite = input(false);
  protected readonly fmtT = fmtT;
  protected readonly fmtUSD = fmtUSD;
  readonly parts = computed(() => {
    const u = this.usage();
    const c = u.costUSD || 1;
    return TOKEN_TYPES.filter((t) => !(t.key === 'cacheWrite' && this.hideCacheWrite())).map((t) => {
      const tokens = t.key === 'cacheWrite' ? u.cacheWrite5m + u.cacheWrite1h : (u[t.key] as number);
      const cost = u[t.cost] as number;
      return { key: t.key, name: t.name, color: t.color, tokens, cost, share: cost / c };
    });
  });
}

/** Findings with severity, evidence link, estimated impact and a fix. */
@Component({
  selector: 'cz-findings',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [HlmBadgeImports, HlmButtonImports],
  host: { class: 'grid gap-2' },
  template: `
    @for (f of findings(); track $index) {
      <div class="grid gap-1 rounded-lg border border-l-[3px] px-3 py-2.5 text-[12.5px]"
        [style.border-left-color]="f.severity === 'high' ? 'var(--crit)' : f.severity === 'medium' ? 'var(--warn)' : 'var(--ok)'">
        <div class="flex items-center gap-2 text-[13px] font-semibold">
          <span hlmBadge [class]="TONE[f.severity]">{{ f.severity === 'high' ? 'High' : f.severity === 'medium' ? 'Medium' : 'OK' }}</span>
          {{ f.title }}
        </div>
        <p class="m-0 text-muted-foreground selectable">{{ f.text }}</p>
        @if (f.fix) {
          <p class="m-0">→ {{ f.fix }}</p>
        }
        @if (f.turn >= 0 || f.impactUSD > 0.005) {
          <p class="m-0 text-muted-foreground">
            @if (f.turn >= 0 && showLinks()) {
              <button hlmBtn variant="link" size="xs" class="h-auto p-0" (click)="state.goTurn(f.turn, f.subagentId || null)">
                Go to {{ f.subagentId ? 'subagent ' : '' }}turn {{ f.turn + 1 }}
              </button>
            }
            @if (f.impactUSD > 0.005) {
              <span>{{ f.turn >= 0 && showLinks() ? ' · ' : '' }}est. impact {{ fmtUSD(f.impactUSD) }}</span>
            }
          </p>
        }
      </div>
    } @empty {
      <p class="text-sm text-muted-foreground">{{ empty() }}</p>
    }
  `,
})
export class FindingsList {
  readonly findings = input.required<ins.Finding[]>();
  readonly showLinks = input(true);
  readonly empty = input('No findings.');
  protected readonly state = inject(AppState);
  protected readonly fmtUSD = fmtUSD;
  protected readonly TONE = TONE;
}

import { ChangeDetectionStrategy, Component, ElementRef, computed, effect, inject, signal, untracked, viewChild } from '@angular/core';
import { SnCommandBarImports, SnStatusPage } from '@spartan-native/desktop-ui';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HistoryService, core } from '../lib/api';
import { errText, fmtUSD, fmtWhen } from '../lib/format';
import { AppState } from '../lib/state';
import { AgentTag, Segmented } from './shared';

const KINDS = [
  { id: 'all', label: 'Everything' },
  { id: 'prompt', label: 'Prompts' },
  { id: 'answer', label: 'Answers' },
  { id: 'command', label: 'Commands' },
  { id: 'file', label: 'Files' },
  { id: 'tool', label: 'Tools' },
] as const;
type Kind = (typeof KINDS)[number]['id'];

const KIND_LABEL: Record<string, string> = { prompt: 'prompt', answer: 'answer', command: 'shell', file: 'file', tool: 'tool' };

/**
 * Full-text search over every transcript in scope: prompts, answers, shell commands, file paths and
 * tool calls. Results are grouped by session; a hit opens the session at its turn (or its step).
 */
@Component({
  selector: 'cz-search',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [SnCommandBarImports, SnStatusPage, HlmInputImports, HlmButtonImports, HlmBadgeImports, AgentTag, Segmented],
  host: { class: 'flex min-h-0 flex-col' },
  template: `
    <sn-command-bar title="Search" titlePosition="start" collapse="none" class="border-b">
      <span snCommandBarEnd class="flex items-center gap-2 pe-2">
        <cz-segmented label="Search in" [options]="scopes" [value]="scope()" (valueChange)="scope.set($event)" />
      </span>
    </sn-command-bar>
    <div class="grid gap-2 border-b px-6 py-3">
      <div class="flex items-center gap-2">
        <input #field hlmInput type="search" class="h-9 min-w-0 flex-1 text-[14px]" placeholder='Search prompts, answers, commands and files. Use "quotes" for a phrase.'
          aria-label="Search transcripts" [value]="state.searchQuery()" (input)="onInput($any($event.target).value)" (keydown.enter)="run()" />
      </div>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <cz-segmented label="Kind" [options]="kinds" [value]="kind()" (valueChange)="kind.set($event)" />
        <span class="text-xs text-muted-foreground" role="status">{{ status() }}</span>
      </div>
    </div>
    <div class="grid min-h-0 flex-1 auto-rows-max content-start gap-3 overflow-auto px-6 pt-3 pb-10 scroll-thin">
      @if (err()) {
        <sn-status-page icon="lucideTriangleAlert" title="Search failed" [description]="err()!" />
      } @else if (res(); as r) {
        @for (g of r.groups ?? []; track g.sourceId + g.sessionId) {
          <section class="grid gap-1 rounded-lg border bg-card px-3 py-2.5">
            <div class="flex min-w-0 items-center gap-2">
              <button type="button" class="min-w-0 cursor-pointer truncate text-left text-[13.5px] font-semibold hover:underline" [title]="g.title" (click)="open(g, null)">{{ g.title }}</button>
              <cz-agent [agent]="g.agent" />
              <span class="ml-auto shrink-0 text-xs text-muted-foreground tabular">{{ g.project }} · {{ fmtWhen(g.endedAt) }} · {{ fmtUSD(g.costUSD) }}</span>
            </div>
            @for (h of g.hits ?? []; track $index) {
              <button type="button" class="grid cursor-pointer grid-cols-[64px_48px_minmax(0,1fr)] items-baseline gap-2 rounded-md px-1.5 py-1 text-left text-[12.5px] hover:bg-accent" (click)="open(g, h)">
                <span class="mono text-[11px] text-muted-foreground">{{ KIND_LABEL[h.kind] }}</span>
                <span class="text-[11px] text-muted-foreground tabular">turn {{ h.turn + 1 }}</span>
                <span class="selectable break-words" [class.mono]="h.kind === 'command' || h.kind === 'file' || h.kind === 'tool'">{{ h.snippet.slice(0, h.matchStart) }}<mark class="rounded-[3px] bg-[color-mix(in_srgb,var(--warn)_35%,transparent)] px-0.5 text-foreground">{{ h.snippet.slice(h.matchStart, h.matchEnd) }}</mark>{{ h.snippet.slice(h.matchEnd) }}</span>
              </button>
            }
            @if (g.total > (g.hits?.length ?? 0)) {
              <span class="px-1.5 text-xs text-muted-foreground">and {{ g.total - (g.hits?.length ?? 0) }} more in this session</span>
            }
          </section>
        } @empty {
          @if (r.query) {
            <sn-status-page icon="lucideSearchX" title="No matches" [description]="'Nothing in ' + r.searched + ' sessions contains every word of “' + r.query + '”.'" />
          }
        }
      } @else if (!state.searchQuery().trim()) {
        <sn-status-page icon="lucideSearch" title="Search every transcript" description="Find a prompt, an answer, a command or a file path across all sessions. The source, agent and title-bar filters apply." />
      }
    </div>
  `,
})
export class Search {
  protected readonly state = inject(AppState);
  protected readonly fmtUSD = fmtUSD;
  protected readonly fmtWhen = fmtWhen;
  protected readonly KIND_LABEL = KIND_LABEL;
  protected readonly kinds = KINDS;
  protected readonly scopes = [
    { id: 'all' as const, label: 'All projects' },
    { id: 'project' as const, label: 'This project' },
  ];
  private readonly field = viewChild<ElementRef<HTMLInputElement>>('field');
  readonly kind = signal<Kind>('all');
  readonly scope = signal<'all' | 'project'>('all');
  readonly res = signal<core.SearchResult | null>(null);
  readonly err = signal<string | null>(null);
  readonly busy = signal(false);
  private seq = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;

  constructor() {
    // Focus on ⌘K (and when the view opens).
    effect(() => {
      this.state.searchFocus();
      const el = this.field()?.nativeElement;
      if (el) setTimeout(() => (el.focus(), el.select()));
    });
    // Re-run when the scope or filters change.
    effect(() => {
      this.kind();
      this.scope();
      this.state.sourceId();
      this.state.agent();
      this.state.filter();
      untracked(() => this.run());
    });
  }

  readonly status = computed(() => {
    if (this.busy()) return 'Searching…';
    const r = this.res();
    if (!r || !r.query) return '';
    const s = `${r.hits} match${r.hits === 1 ? '' : 'es'} in ${r.groups?.length ?? 0} of ${r.searched} sessions · ${r.ms} ms`;
    return (r.truncated ? s + ' · showing the first 300' : s) + (r.failed ? ` · ${r.failed} unreadable` : '');
  });

  onInput(v: string): void {
    this.state.searchQuery.set(v);
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.run(), 250);
  }

  async run(): Promise<void> {
    clearTimeout(this.timer);
    const q = this.state.searchQuery().trim();
    const n = ++this.seq;
    this.err.set(null);
    if (q.length < 2) {
      this.res.set(null);
      this.busy.set(false);
      return;
    }
    this.busy.set(true);
    try {
      const key = this.scope() === 'project' ? (this.state.projectKey() ?? '') : '';
      const r = await HistoryService.Search(q, key, this.state.sourceId(), this.state.agent(), this.kind() === 'all' ? '' : this.kind(), this.state.filter());
      if (n === this.seq) this.res.set(r);
    } catch (e) {
      if (n === this.seq) this.err.set(errText(e));
    } finally {
      if (n === this.seq) this.busy.set(false);
    }
  }

  async open(g: core.SearchGroup, h: core.SearchHit | null): Promise<void> {
    await this.state.openFrom(g.projectKey, g.sourceId, g.sessionId, h ? h.turn : null);
    if (h?.stepId) this.state.openStep(h.stepId, h.turn);
  }
}

import { Injectable, signal } from '@angular/core';
import {
  TTSPlaylist,
  TTSStatus,
  TTSTest,
  TTSArticleStatus,
  TTSSynthesizeArticle,
} from '../../../wailsjs/go/bindings/Bindings';
import { tts } from '../../../wailsjs/go/models';
import { SettingService } from './setting.service';

/**
 * Narration playback for freshly fetched articles.
 *
 * The collector's cycle hook hands each round's new article IDs to the backend
 * TTS service, which synthesises mp3s asynchronously. This service polls
 * TTSStatus while a board that owns the feature is open, auto-starts the
 * playlist when a fresh round reaches "ready", and owns the single <audio>
 * element the player bar drives.
 *
 * Per-article controls: each card can query whether its audio exists or is
 * being synthesised, and can trigger on-demand synthesis if missing.
 */
@Injectable({ providedIn: 'root' })
export class PlaybackService {
  /** Latest round snapshot (state, done/total, roundId). */
  readonly status = signal<tts.Status | null>(null);
  /** Playlist of the round that finished synthesising. */
  readonly queue = signal<tts.PlaylistItem[]>([]);
  readonly index = signal(0);
  readonly current = signal<tts.PlaylistItem | null>(null);
  readonly playing = signal(false);
  /** WebView2 rejected an unmuted play(): the bar shows a click-to-start hint. */
  readonly blocked = signal(false);
  /** Playback position of the current clip, 0..1. */
  readonly progress = signal(0);

  private readonly audio = new Audio();
  private timer: ReturnType<typeof setTimeout> | null = null;
  /** Number of live watchers; the poll lives exactly as long as one exists. */
  private watchers = 0;
  /** Highest round this client already evaluated for auto-play. */
  private lastAutoRound = 0;

  /** Per-article synthesis status cache: articleId -> { hasAudio, synthesizing }. */
  private readonly articleStatus = new Map<number, { hasAudio: boolean; synthesizing: boolean }>();

  constructor(private settingService: SettingService) {
    this.audio.preload = 'auto';
    this.audio.addEventListener('ended', () => this.next());
    this.audio.addEventListener('timeupdate', () => {
      const d = this.audio.duration;
      this.progress.set(d > 0 ? this.audio.currentTime / d : 0);
    });
    this.audio.addEventListener('error', () => {
      // A missing file must not stall the playlist: skip to the next clip.
      if (this.index() + 1 < this.queue().length) this.next();
      else this.playing.set(false);
    });
  }

  // ---- round watcher (started by pages that want auto-play) ----

  startWatch() {
    this.watchers++;
    if (this.timer === null) {
      this.schedule(0);
    }
  }

  stopWatch() {
    this.watchers = Math.max(0, this.watchers - 1);
    if (this.watchers === 0 && this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
  }

  private schedule(ms: number) {
    this.timer = setTimeout(() => {
      this.timer = null;
      void this.tick();
    }, ms);
  }

  private async tick() {
    let synthesizing = false;
    try {
      const st = await TTSStatus();
      this.status.set(st);
      synthesizing = st.state === 'synthesizing';
      if (st.state === 'ready' && st.roundId > this.lastAutoRound) {
        this.lastAutoRound = st.roundId;
        await this.maybeAutoPlay(st);
      }
    } catch (e) {
      console.error('TTS status poll failed:', e);
    }
    if (this.watchers > 0) {
      this.schedule(synthesizing ? 1000 : 3000);
    }
  }

  private async maybeAutoPlay(st: tts.Status) {
    // Only a round that finished recently: opening the app an hour later must
    // not blast yesterday's news.
    const finished = Date.parse(String(st.finishedAt));
    if (!Number.isFinite(finished) || Date.now() - finished > 15 * 60_000) {
      return;
    }
    try {
      const all = await this.settingService.getAll();
      if (all['tts_auto_play'] !== 'true') {
        return;
      }
      const pl = await TTSPlaylist();
      // Never interrupt audio the user already started by hand.
      if (!pl.length || this.playing()) {
        return;
      }
      this.queue.set(pl);
      this.index.set(0);
      this.play();
    } catch (e) {
      console.error('TTS auto-play failed:', e);
    }
  }

  // ---- per-article status ----

  /**
   * Returns whether the given article already has audio and whether it is
   * currently being synthesised as part of an in-flight round.
   */
  async getArticleStatus(articleId: number): Promise<{ hasAudio: boolean; synthesizing: boolean }> {
    const cached = this.articleStatus.get(articleId);
    if (cached) return cached;
    try {
      const result = await TTSArticleStatus(articleId);
      const mapped = { hasAudio: result.hasAudio, synthesizing: result.synthesizing };
      this.articleStatus.set(articleId, mapped);
      return mapped;
    } catch {
      return { hasAudio: false, synthesizing: false };
    }
  }

  /**
   * Triggers on-demand synthesis for a single article. Returns true once the
   * audio is available (or was already present). The caller should refresh
   * the article status afterwards.
   */
  async synthesizeArticle(articleId: number): Promise<void> {
    try {
      await TTSSynthesizeArticle(articleId);
      // Invalidate cache so next status check reflects the new file.
      this.articleStatus.delete(articleId);
    } catch (e) {
      console.error(`[playback] synthesize article ${articleId} failed:`, e);
      throw e;
    }
  }

  /**
   * Plays the article with the given ID, skipping any article that has no
   * audio file. Returns false if the article has no audio and synthesis was
   * not triggered (caller should show a "synthesize" button instead).
   */
  async playArticle(articleId: number): Promise<boolean> {
    const item = this.queue().find((it) => it.articleId === articleId);
    if (item) {
      // Article is in the current round playlist — play from playlist position.
      const idx = this.queue().indexOf(item);
      this.index.set(idx);
      this.play();
      return true;
    }
    // Article not in playlist: check if audio file exists directly.
    const { hasAudio } = await this.getArticleStatus(articleId);
    if (!hasAudio) return false;
    // Play directly from the media URL.
    this.stop();
    const url = `/media/tts/${articleId}.mp3`;
    this.audio.src = url;
    this.current.set({ articleId, title: '', url, source: '' });
    try {
      await this.audio.play();
      this.playing.set(true);
      this.blocked.set(false);
      return true;
    } catch {
      this.playing.set(false);
      this.blocked.set(true);
      return false;
    }
  }

  // ---- transport ----

  /**
   * Manual play requested by a board's play button: ignores the auto-play
   * setting and the 15-minute freshness window (those gate *automatic*
   * starts only), but never interrupts audio the user already started.
   */
  async playRound() {
    try {
      if (this.queue().length && !this.playing()) {
        this.play();
        return;
      }
      if (this.playing()) return;
      const st = await TTSStatus();
      this.status.set(st);
      if (st.state !== 'ready') {
        this.notifyEmpty(st);
        return;
      }
      const pl = await TTSPlaylist();
      if (!pl.length) {
        this.notifyEmpty(st);
        return;
      }
      this.queue.set(pl);
      this.index.set(0);
      this.play();
    } catch (e) {
      console.error('TTS manual play failed:', e);
    }
  }

  /** Stop playback and drop the playlist (next play starts the round over). */
  stop() {
    this.audio.pause();
    this.audio.removeAttribute('src');
    this.audio.load();
    this.playing.set(false);
    this.blocked.set(false);
    this.progress.set(0);
    this.queue.set([]);
    this.index.set(0);
    this.current.set(null);
  }

  private notifyEmpty(st: tts.Status) {
    // Surface why there is nothing to play: synthesis still running vs. an
    // empty/never-run round.
    this.status.set(st);
    this.blocked.set(st.state === 'synthesizing');
  }

  play() {
    const item = this.queue()[this.index()];
    this.current.set(item ?? null);
    if (!item) {
      this.playing.set(false);
      return;
    }
    const want = new URL(item.url, document.baseURI).href;
    if (this.audio.src !== want) {
      this.audio.src = item.url;
      this.progress.set(0);
    }
    this.audio.play().then(
      () => {
        this.playing.set(true);
        this.blocked.set(false);
      },
      () => {
        // Autoplay policy: no sound before a user gesture. The play button on
        // the bar provides one, so the next attempt succeeds.
        this.playing.set(false);
        this.blocked.set(true);
      }
    );
  }

  pause() {
    this.audio.pause();
    this.playing.set(false);
  }

  toggle() {
    if (this.playing()) this.pause();
    else this.play();
  }

  next() {
    if (this.index() + 1 < this.queue().length) {
      this.index.update((i) => i + 1);
      this.play();
      return;
    }
    // End of the round: park at the last clip, reset the bar.
    this.audio.pause();
    this.playing.set(false);
    this.progress.set(0);
  }

  prev() {
    if (this.audio.currentTime > 3 || this.index() === 0) {
      this.audio.currentTime = 0;
      this.progress.set(0);
      if (!this.playing()) this.play();
      return;
    }
    this.index.update((i) => i - 1);
    this.play();
  }

  seek(frac: number) {
    const d = this.audio.duration;
    if (!Number.isFinite(d) || d <= 0) return;
    this.audio.currentTime = Math.min(d, Math.max(0, frac * d));
    this.progress.set(Math.min(1, Math.max(0, frac)));
  }

  // ---- one-shot API wrappers used by the settings page ----

  async test(): Promise<string> {
    return TTSTest();
  }
}

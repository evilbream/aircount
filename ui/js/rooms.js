// One card per sensor: the answer to "is anyone moving there right now?", without
// having to read a number against a threshold. Tapping a card hides that sensor
// on the chart, which also lets the axis refit to the sensors that are left.
//
// The state comes from the latest raw window, never from a chart bucket: on the
// 24h view a bucket holds 4 minutes of max, and the card would keep saying
// "Motion" long after the room went quiet.
//
// Times here are arrival times rather than ev.ts, so a sensor with a drifting
// clock cannot read as silent while its data is plainly coming in. The one
// exception is "motion N ago" seeded from history: there is no arrival time for
// it, so it uses observed_at, which the server stamps with its own clock.

import { fmtDur, fmtScore } from './series.js';

function span(cls) {
  const el = document.createElement('span');
  el.className = cls;
  return el;
}

export class Rooms {
  constructor({ el, palette, threshold, staleAfterMs, onToggle }) {
    this.el = el;
    this.palette = palette;
    this.threshold = threshold;
    this.staleAfterMs = staleAfterMs;
    this.onToggle = onToggle;
    this.rooms = new Map(); // sensorId -> { score, seenAt, motionAt, shown, ...nodes }
  }

  /** Cards for the sensors we are subscribed to, before any of them has spoken. */
  setSensors(ids) {
    for (const id of ids) this.#room(id);
    this.render();
  }

  add(id, score) {
    const room = this.#room(id);
    room.score = score;
    room.seenAt = Date.now();
    if (score >= this.threshold) room.motionAt = room.seenAt;
  }

  /**
   * Last motion known from history. Only the "motion N ago" note: the state
   * itself waits for the live stream, because ClickHouse lags it by minutes and
   * its last point would make a busy room read as Silent until then.
   */
  seedMotion(id, atMs) {
    const room = this.#room(id);
    if (room.motionAt == null || atMs > room.motionAt) room.motionAt = atMs;
  }

  render() {
    const now = Date.now();
    for (const room of this.rooms.values()) {
      const { cls, state, note, score } = this.#describe(room, now);
      room.card.className = `room ${cls}`;
      room.state.textContent = state;
      room.note.textContent = note;
      room.value.textContent = fmtScore(score);
    }
  }

  #describe({ score, seenAt, motionAt }, now) {
    if (seenAt == null) {
      const note = motionAt == null ? 'no data yet' : `motion ${fmtDur(now - motionAt)} ago`;
      return { cls: 'waiting', state: 'Waiting', note };
    }
    const seen = fmtDur(now - seenAt);
    if (now - seenAt > this.staleAfterMs) {
      return { cls: 'silent', state: 'Silent', note: `last data ${seen} ago` };
    }
    if (score >= this.threshold) {
      return { cls: 'motion', state: 'Motion', note: `updated ${seen} ago`, score };
    }
    const note = motionAt == null ? 'no motion yet' : `motion ${fmtDur(now - motionAt)} ago`;
    return { cls: 'quiet', state: 'Quiet', note, score };
  }

  #room(id) {
    let room = this.rooms.get(id);
    if (room) return room;

    const card = document.createElement('button');
    card.type = 'button';
    card.setAttribute('aria-pressed', 'true');
    card.title = 'Show or hide this sensor on the chart';

    const name = span('room-name');
    const swatch = span('swatch');
    swatch.style.background = this.palette.colorFor(id);
    const label = span('room-label');
    label.textContent = id;
    name.append(swatch, label);

    room = {
      id, card,
      state: span('room-state'), value: span('room-score'), note: span('room-note'),
      score: null, seenAt: null, motionAt: null, shown: true,
    };
    card.append(name, room.state, room.value, room.note);

    card.addEventListener('click', () => {
      room.shown = !room.shown;
      card.setAttribute('aria-pressed', room.shown);
      this.onToggle?.(id, room.shown);
    });

    this.rooms.set(id, room);
    // Same order as the chart, however the sensors happened to arrive.
    const sorted = [...this.rooms.values()].sort((a, b) => (a.id < b.id ? -1 : 1));
    this.el.replaceChildren(...sorted.map((r) => r.card));
    return room;
  }
}

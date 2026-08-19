import './style.css';

import { Analyze, AnalyzerPreparing, AudioDuration, ChooseAudioFile, ChooseTimelineSavePath, DiscoverDevices, GenerateFromAnalysis, MasterBrightness, PausePreview, ResumePreview, SaveTimeline, SetMasterBrightness, StartAudioPreview, StartPreview, StopPreview, Styles } from '../wailsjs/go/main/App';

// The walkthrough only appears while the bundled analyzer is preparing itself,
// which is the one moment there is a wait worth filling. Set LIFX_MAESTRO_FORCE_TOUR
// to preview it in a development build, which has no analyzer to prepare.
type TourStep = {
  // First selector that matches wins, so a step still lands when the UI is in a
  // different state (a collapsed sidebar, a control not yet enabled).
  anchors: string[];
  title: string;
  body: string;
  placement: 'below' | 'right';
};

const TOUR_STEPS: TourStep[] = [
  {
    anchors: ['#choose-song'],
    title: 'Choose a song',
    body: 'Pick an MP3 or WAV file to build a light show from.',
    placement: 'below',
  },
  {
    anchors: ['#style'],
    title: 'Pick a style',
    body: 'The style decides which effects get used, from minimal pulses to full synthwave sweeps.',
    placement: 'below',
  },
  {
    anchors: ['#regenerate'],
    title: 'Generate',
    body: 'Analyses the track for tempo, beats and sections, then builds an editable timeline.',
    placement: 'below',
  },
  {
    anchors: ['.sidebar .device', '.sidebar', '#toggle-sidebar'],
    title: 'Select targets',
    body: 'Choose which lights the timeline drives. Generate again after changing your selection.',
    placement: 'right',
  },
  {
    anchors: ['#play-toggle'],
    title: 'Play',
    body: 'Plays the audio and drives your lights from the same clock, so they stay in sync.',
    placement: 'below',
  },
];

type DeviceKind = 'single_zone' | 'multi_zone' | 'matrix' | 'switch';

type DeviceInfo = {
  id: string;
  label: string;
  group: string;
  location: string;
  capabilities: {
    kind: DeviceKind;
    has_color: boolean;
    has_kelvin: boolean;
    zone_count: number;
    matrix_width: number;
    matrix_height: number;
    matrix_length: number;
  };
};

type AnalysisSection = {
  start_ms: number;
  end_ms: number;
  type: string;
  energy: number;
};

type EnergyPoint = {
  time_ms: number;
  value: number;
};

type TimelineEvent = {
  time_ms: number;
  target: string;
  action: string;
  params?: Record<string, any>;
};

type Timeline = {
  name: string;
  duration_ms: number;
  events: TimelineEvent[];
};

type EditorSession = {
  song_path: string;
  song_name: string;
  style: string;
  target: string;
  analysis: {
    duration_ms: number;
    bpm: number;
    beats: number[];
    energy: EnergyPoint[];
    sections?: AnalysisSection[];
  };
  timeline: Timeline;
  devices: DeviceInfo[];
  summary: {
    bpm: number;
    duration_ms: number;
    beats: number;
    sections: number;
    events: number;
  };
  source: string;
  event_stats: Record<string, number>;
};

type AppState = {
  session: EditorSession | null;
  devices: DeviceInfo[];
  styles: string[];
  selectedEvent: number;
  selectedDevice: string;
  selectedAudioPath: string;
  targetTokens: string[];
  playheadMS: number;
  playing: boolean;
  previewPaused: boolean;
  status: string;
  loading: boolean;
  // Index into TOUR_STEPS, or null when the walkthrough is not showing.
  tourStep: number | null;
  // Output level as a percentage. Applied while playing, so it never invalidates
  // a generated timeline.
  masterBrightness: number;
  previewStarting: boolean;
  needsRegeneration: boolean;
  regenerationReasons: {
    style: boolean;
    target: boolean;
    devices: boolean;
  };
  regenerationPrompt: boolean;
  generatedStyle: string;
  zoomPxPerSecond: number;
  inspectorOpen: boolean;
  inspectorWidth: number;
  sidebarOpen: boolean;
  timelineScrollLeft: number;
  timelineScrollTop: number;
};

const state: AppState = {
  session: null,
  devices: [],
  styles: [],
  selectedEvent: -1,
  selectedDevice: 'all',
  selectedAudioPath: '',
  targetTokens: [],
  playheadMS: 0,
  playing: false,
  previewPaused: false,
  status: 'Loading editor',
  loading: false,
  tourStep: null,
  masterBrightness: 100,
  previewStarting: false,
  needsRegeneration: false,
  regenerationReasons: {
    style: false,
    target: false,
    devices: false,
  },
  regenerationPrompt: false,
  generatedStyle: '',
  zoomPxPerSecond: 16,
  inspectorOpen: false,
  inspectorWidth: 306,
  sidebarOpen: true,
  timelineScrollLeft: 0,
  timelineScrollTop: 0,
};

let playTimer: number | undefined;
let playbackStartedAt = 0;

const appRoot = document.querySelector<HTMLDivElement>('#app');
if (!appRoot) {
  throw new Error('app root is missing');
}
const app = appRoot;

void bootstrap();

// The card and highlight are placed in viewport coordinates, so a resize has to
// re-place them. Registered once, outside render, which rebuilds the DOM.
window.addEventListener('resize', positionTour);

// Whether the first-run walkthrough is worth showing. A failure here should never
// keep the app from starting, so treat it as "nothing to explain".
async function analyzerPreparing() {
  try {
    return await AnalyzerPreparing();
  } catch {
    return false;
  }
}

async function bootstrap() {
  state.tourStep = (await analyzerPreparing()) ? 0 : null;
  try {
    state.masterBrightness = await MasterBrightness();
  } catch {
    state.masterBrightness = 100;
  }
  try {
    state.styles = await Styles();
    state.status = 'Discovering LIFX LAN devices';
    render();
    state.devices = await DiscoverDevices() as unknown as DeviceInfo[];
    state.targetTokens = ['all'];
    state.status = `Discovered ${state.devices.length} LIFX devices`;
  } catch (error) {
    state.devices = [];
    state.targetTokens = ['all'];
    state.status = `Device discovery failed: ${readableError(error)}`;
  }
  render();
}

function render() {
  captureTimelineScroll();
  app.innerHTML = `
    <div class="shell" style="--inspector-width:${state.inspectorOpen ? state.inspectorWidth : 0}px;">
      ${renderToolbar()}
      <div class="workspace ${state.inspectorOpen ? '' : 'inspector-closed'} ${state.sidebarOpen ? '' : 'sidebar-closed'}">
        ${renderTargets()}
        <main class="timeline-panel">
          ${renderOverview()}
          ${renderTimeline()}
          ${renderAnalysis()}
        </main>
        ${renderInspector()}
      </div>
      ${renderOverlay()}
      ${renderTour()}
    </div>
  `;
  bindEvents();
  restoreTimelineScroll();
  // Anchor positions come from the DOM that was just built, so they can never go
  // stale against a re-render.
  positionTour();
}

function renderToolbar() {
  const session = state.session;
  const styleOptions = state.styles
    .map((style) => `<option value="${style}" ${session?.style === style ? 'selected' : ''}>${style}</option>`)
    .join('');
  return `
    <header class="toolbar">
      <div class="brand">
        <div class="mark"></div>
        <div>
          <div class="product">lifx-maestro</div>
        </div>
      </div>
      <div class="transport">
        <button id="play-toggle" class="tool transport-button" ${state.previewStarting || !selectedSongPath() ? 'disabled' : ''}>${state.previewStarting ? 'Starting' : state.playing ? 'Pause' : 'Play'}</button>
        <button id="stop" class="tool transport-button" ${state.previewStarting || !selectedSongPath() ? 'disabled' : ''}>Stop</button>
        <div class="timecode">${formatTime(state.playheadMS)} / ${formatTime(session ? playbackDurationMS(session) : 0)}</div>
      </div>
      <div class="actions">
        <label class="field">
          <span>Brightness</span>
          <span class="brightness-field">
            <input id="master-brightness" class="brightness-slider" type="range" min="5" max="100" step="5" value="${state.masterBrightness}" />
            <span class="brightness-value">${Math.round(state.masterBrightness)}%</span>
          </span>
        </label>
        <label class="field">
          <span>Style</span>
          <select id="style" class="select-control">${styleOptions}</select>
        </label>
        <button id="choose-song" class="tool primary" ${state.loading ? 'disabled' : ''}>Choose Song</button>
        <button id="regenerate" class="tool ${state.needsRegeneration ? 'attention' : ''}" ${state.loading || !selectedSongPath() ? 'disabled' : ''}>${state.needsRegeneration ? 'Regenerate' : 'Generate'}</button>
        <button id="save" class="tool" ${!session ? 'disabled' : ''}>Save JSON</button>
      </div>
    </header>
  `;
}

function renderOverlay() {
  if (state.regenerationPrompt) {
    return `
      <div class="busy-overlay">
        <div class="busy-modal prompt-modal">
          <strong>Regenerate timeline</strong>
          <span>${escapeHTML(regenerationMessage())}</span>
          <div class="modal-actions">
            <button id="prompt-cancel" class="tool">Cancel</button>
            <button id="prompt-regenerate" class="tool attention">Regenerate</button>
          </div>
        </div>
      </div>
    `;
  }
  if (!state.loading && !state.previewStarting) {
    return '';
  }
  return `
    <div class="busy-overlay">
      <div class="busy-modal">
        <div class="spinner"></div>
        <span>${escapeHTML(state.status)}</span>
      </div>
    </div>
  `;
}

function renderTour() {
  const step = state.tourStep === null ? undefined : TOUR_STEPS[state.tourStep];
  if (!step) {
    return '';
  }
  // Stand aside for the spinner and prompts, which own the screen when they show.
  if (state.loading || state.previewStarting || state.regenerationPrompt) {
    return '';
  }

  const last = state.tourStep === TOUR_STEPS.length - 1;
  return `
    <div class="tour-layer">
      <div class="tour-ring" id="tour-ring"></div>
      <div class="tour-card" id="tour-card">
        <div class="tour-progress">Step ${(state.tourStep ?? 0) + 1} of ${TOUR_STEPS.length}</div>
        <strong>${escapeHTML(step.title)}</strong>
        <span>${escapeHTML(step.body)}</span>
        <div class="tour-actions">
          <button id="tour-skip" class="tool">Skip</button>
          <button id="tour-next" class="tool attention">${last ? 'Done' : 'Next'}</button>
        </div>
      </div>
    </div>
  `;
}

// Positions the highlight and card against the live anchor. Steps advance only on
// click, so a step whose control is disabled or hidden still reads sensibly.
function positionTour() {
  const step = state.tourStep === null ? undefined : TOUR_STEPS[state.tourStep];
  const ring = document.querySelector<HTMLElement>('#tour-ring');
  const card = document.querySelector<HTMLElement>('#tour-card');
  if (!step || !ring || !card) {
    return;
  }

  const anchor = step.anchors.reduce<Element | null>(
    (found, selector) => found ?? document.querySelector(selector),
    null,
  );

  const gap = 12;
  if (!anchor) {
    // Nothing on screen to point at, so present the step on its own.
    ring.style.display = 'none';
    card.style.left = `${Math.round((window.innerWidth - card.offsetWidth) / 2)}px`;
    card.style.top = `${Math.round((window.innerHeight - card.offsetHeight) / 2)}px`;
    return;
  }

  const rect = anchor.getBoundingClientRect();
  const pad = 6;
  ring.style.display = 'block';
  ring.style.left = `${rect.left - pad}px`;
  ring.style.top = `${rect.top - pad}px`;
  ring.style.width = `${rect.width + pad * 2}px`;
  ring.style.height = `${rect.height + pad * 2}px`;

  const preferred = step.placement === 'right'
    ? { left: rect.right + gap, top: rect.top }
    : { left: rect.left, top: rect.bottom + gap };
  card.style.left = `${Math.round(clamp(preferred.left, gap, window.innerWidth - card.offsetWidth - gap))}px`;
  card.style.top = `${Math.round(clamp(preferred.top, gap, window.innerHeight - card.offsetHeight - gap))}px`;
}

function advanceTour() {
  if (state.tourStep === null || state.tourStep >= TOUR_STEPS.length - 1) {
    endTour();
    return;
  }
  state.tourStep += 1;
  render();
}

function endTour() {
  state.tourStep = null;
  render();
}

function renderTargets() {
  if (!state.sidebarOpen) {
    return `<aside class="sidebar collapsed"><button id="toggle-sidebar" class="panel-toggle" title="Show targets">›</button></aside>`;
  }
  const session = state.session;
  const devices = session?.devices ?? state.devices;
  const groups = unique(devices.map((device) => device.group).filter(Boolean));
  const locations = unique(devices.map((device) => device.location).filter(Boolean));
  const deviceItems = devices.map((device) => {
    const selected = targetIncludes(device.id) ? 'selected' : '';
    return `
      <button class="device ${selected}" data-device="${escapeAttr(device.id)}">
        <span class="device-toggle"></span>
        <span class="device-main">
          <strong>${escapeHTML(device.label || device.id)}</strong>
          <small>${escapeHTML(device.group)} / ${escapeHTML(device.location)}</small>
        </span>
        <span class="badge">${capabilityLabel(device)}</span>
      </button>
    `;
  }).join('');

  return `
    <aside class="sidebar">
      <div class="sidebar-head">
        <div class="panel-control-row right">
          <button id="discover-devices" class="icon-tool" ${state.loading ? 'disabled' : ''} title="Refresh devices">↻</button>
          <button id="toggle-sidebar" class="panel-toggle" title="Hide targets">‹</button>
        </div>
        <div>
          <div class="panel-title">Targets</div>
          <div class="sidebar-note">${devices.length > 0 ? 'Discovered devices' : 'No devices discovered'}</div>
        </div>
      </div>
      <button class="device ${targetIncludes('all') ? 'selected' : ''}" data-target-token="all">
        <span class="device-toggle"></span>
        <span class="device-main">
          <strong>All targets</strong>
          <small>All matching devices</small>
        </span>
        <span class="badge">mix</span>
      </button>
      ${renderTokenGroup('Groups', groups)}
      ${renderTokenGroup('Locations', locations)}
      <div class="token-title device-title">Devices</div>
      ${deviceItems}
    </aside>
  `;
}

function renderOverview() {
  const session = state.session;
  if (!session) {
    return `<section class="overview empty">No timeline loaded</section>`;
  }
  const visibleEvents = timelineForSelectedTargets(session).events.length;
  return `
    <section class="overview">
      <div>
        <h1>${escapeHTML(session.song_name)}</h1>
        <p>${overviewSubtitle(session)}</p>
      </div>
      <div class="summary-grid">
        <div><span>Duration</span><strong>${formatTime(session.summary.duration_ms)}</strong></div>
        <div><span>BPM</span><strong>${formatNumber(session.summary.bpm, 1)}</strong></div>
        <div><span>Events</span><strong>${visibleEvents}</strong></div>
        <div><span>Beats</span><strong>${session.summary.beats}</strong></div>
        <div><span>Sections</span><strong>${session.summary.sections}</strong></div>
      </div>
    </section>
  `;
}

function renderTokenGroup(title: string, values: string[]) {
  if (values.length === 0) {
    return '';
  }
  return `
    <div class="token-group">
      <div class="token-title">${escapeHTML(title)}</div>
      <div class="token-list">
        ${values.map((value) => `
          <button class="target-token ${targetIncludes(value) ? 'selected' : ''}" data-target-token="${escapeAttr(value)}">${escapeHTML(value)}</button>
        `).join('')}
      </div>
    </div>
  `;
}

function renderTimeline() {
  const session = state.session;
  if (!session) {
    return `<section class="timeline empty">Choose a song to generate a choreography.</section>`;
  }

  const duration = Math.max(visibleTimelineDurationMS(session), 1);
  const timelineWidth = timelineWidthPx(duration);
  const sections = (session.analysis.sections ?? []).map((section) => {
    const left = timeToX(section.start_ms);
    const width = Math.max(timeToX(section.end_ms - section.start_ms), 8);
    return `<div class="section-band ${escapeAttr(section.type)}" style="left:${left}px;width:${width}px">${escapeHTML(section.type)}</div>`;
  }).join('');

  const beats = session.analysis.beats
    .filter((_, index) => index % 4 === 0)
    .map((beat) => `<span class="beat" style="left:${timeToX(beat)}px"></span>`)
    .join('');

  const lanes = laneIDs(session).map((lane) => renderLane(session, lane, timelineWidth)).join('');
  const ticks = renderTicks(duration);
  const playhead = `<div class="playhead" style="left:${timeToX(state.playheadMS)}px"></div>`;

  return `
    <section class="timeline">
      <div class="timeline-tools">
        <div class="panel-title">Timeline</div>
        <div class="zoom-control">
          <button id="zoom-out" class="icon-tool">-</button>
          <input id="timeline-zoom" type="range" min="4" max="80" value="${state.zoomPxPerSecond}" />
          <button id="zoom-in" class="icon-tool">+</button>
          <span>${Math.round(state.zoomPxPerSecond)} px/s</span>
        </div>
      </div>
      <div class="timeline-scroll">
        <div class="ruler" style="--timeline-width:${timelineWidth}px;">
          <div class="ruler-label"></div>
          <div class="ruler-content">
            <div class="section-track">${sections}</div>
            <div class="tick-track">${ticks}${beats}</div>
          </div>
        </div>
        <div class="lanes" style="--timeline-width:${timelineWidth}px;">
          <div class="playhead-area">${playhead}</div>
          ${lanes}
        </div>
      </div>
    </section>
  `;
}

function renderLane(session: EditorSession, lane: string, timelineWidth: number) {
  const device = session.devices.find((candidate) => candidate.id === lane);
  const label = device?.label ?? lane;
  const events = session.timeline.events
    .map((event, index) => ({ event, index }))
    .filter(({ event }) => eventAppliesToLane(event, device, lane))
    .map(({ event, index }) => {
      const eventDuration = eventDurationMS(event) || 180;
      const left = timeToX(event.time_ms);
      const width = Math.max(timeToX(eventDuration), event.action === 'power_on' ? 8 : 14);
      const eventBackground = eventBackgroundStyle(event);
      const selected = index === state.selectedEvent ? 'selected' : '';
      return `
        <button
          class="event ${selected} ${escapeAttr(event.action)}"
          data-event="${index}"
          draggable="true"
          title="${escapeAttr(event.action)} ${formatTime(event.time_ms)}"
          style="left:${left}px;width:${width}px;${eventBackground}"
        >
          <span>${shortAction(event.action)}</span>
        </button>
      `;
    }).join('');

  return `
    <div class="lane">
      <div class="lane-label">${escapeHTML(label)}</div>
      <div class="lane-events" style="width:${timelineWidth}px">${events}</div>
    </div>
  `;
}

function renderAnalysis() {
  const session = state.session;
  if (!session) {
    return '';
  }
  const duration = Math.max(session.timeline.duration_ms, 1);
  const energyPath = session.analysis.energy.map((point, index) => {
    const x = percent(point.time_ms, duration);
    const y = 100 - point.value * 100;
    return `${index === 0 ? 'M' : 'L'} ${x.toFixed(2)} ${y.toFixed(2)}`;
  }).join(' ');
  const sections = (session.analysis.sections ?? []).map((section) => `
    <div class="section-row">
      <strong>${escapeHTML(section.type)}</strong>
      <span>${formatTime(section.start_ms)} - ${formatTime(section.end_ms)}</span>
      <meter min="0" max="1" value="${section.energy}"></meter>
    </div>
  `).join('');

  return `
    <section class="analysis">
      <div class="energy">
        <div class="panel-title">Energy</div>
        <svg viewBox="0 0 100 100" preserveAspectRatio="none">
          <path d="${energyPath}" />
        </svg>
      </div>
      <div class="sections-list">
        <div class="panel-title">Sections</div>
        ${sections}
      </div>
    </section>
  `;
}

function renderInspector() {
  const session = state.session;
  const event = selectedEvent();
  if (!state.inspectorOpen) {
    return `<aside class="inspector collapsed"><button id="toggle-inspector" class="panel-toggle" title="Show editor">‹</button></aside>`;
  }
  if (!session || !event) {
    return `
      <aside class="inspector">
        <div class="inspector-head">
          <div class="panel-control-row left">
            <button id="toggle-inspector" class="panel-toggle" title="Hide editor">›</button>
          </div>
          <div class="panel-title">Editor</div>
        </div>
        <div id="inspector-resize" class="resize-handle"></div>
        <div class="empty-copy">Select a timeline event to edit timing, target, color, brightness, or transition duration.</div>
      </aside>
    `;
  }
  const params = event.params ?? {};
  const hue = colorParam(params, 'hue', 240);
  const saturation = percentParam(params, 'saturation', 100);
  const brightness = percentParam(params, 'brightness', 70);
  const kelvin = colorParam(params, 'kelvin', 3500);
  const durationMS = durationParam(params, 180);
  const targetLabel = eventTargetLabel(session, event);
  const spatial = spatialEventSummary(event);
  const colorControlsDisabled = spatial ? 'disabled' : '';
  const colorControlHint = spatial ? `<div class="field-note">Gradient editing is not available yet. Duplicate or delete the event, or regenerate the timeline to change this gradient.</div>` : '';

  return `
    <aside class="inspector">
      <div class="inspector-head">
        <div class="panel-control-row left">
          <button id="toggle-inspector" class="panel-toggle" title="Hide editor">›</button>
        </div>
        <div class="panel-title">Editor</div>
      </div>
      <div id="inspector-resize" class="resize-handle"></div>
      <div class="color-editor">
        <div class="swatch ${spatial ? 'gradient' : ''}" style="${eventBackgroundStyle(event)}"></div>
        <div class="color-readout">
          ${spatial ? `
            <strong>${escapeHTML(spatial.title)}</strong>
            <span>${escapeHTML(spatial.detail)}</span>
            <span>${escapeHTML(spatial.colors)}</span>
          ` : `
            <strong>${Math.round(hue)}°</strong>
            <span>${Math.round(saturation)}% sat · ${Math.round(brightness)}% bri</span>
            <span>${Math.round(kelvin)} K</span>
          `}
        </div>
      </div>
      <label class="edit-field">
        <span>Target</span>
        <input value="${escapeAttr(targetLabel)}" readonly />
      </label>
      <label class="edit-field">
        <span>Time (ms)</span>
        <input id="event-time" type="number" min="0" max="${session.timeline.duration_ms}" value="${event.time_ms}" />
      </label>
      <label class="edit-field">
        <span>Hue</span>
        <input id="event-hue" class="hue-range" type="range" min="0" max="360" value="${hue}" ${colorControlsDisabled} />
      </label>
      <label class="edit-field">
        <span>Saturation</span>
        <input id="event-saturation" type="range" min="0" max="100" value="${saturation}" ${colorControlsDisabled} />
      </label>
      <label class="edit-field">
        <span>Brightness</span>
        <input id="event-brightness" type="range" min="1" max="100" value="${brightness}" ${colorControlsDisabled} />
      </label>
      <label class="edit-field">
        <span>Kelvin</span>
        <input id="event-kelvin" type="number" min="1500" max="9000" step="100" value="${kelvin}" ${colorControlsDisabled} />
      </label>
      ${colorControlHint}
      <label class="edit-field">
        <span>Duration (ms)</span>
        <input id="event-duration" type="number" min="0" step="10" value="${durationMS}" />
      </label>
      <div class="inspector-actions">
        <button id="duplicate-event" class="tool icon-action" title="Duplicate event" aria-label="Duplicate event">⧉</button>
        <button id="delete-event" class="tool danger icon-action" title="Delete event" aria-label="Delete event">×</button>
      </div>
    </aside>
  `;
}

function bindEvents() {
  document.querySelector('#choose-song')?.addEventListener('click', chooseSong);
  document.querySelector('#regenerate')?.addEventListener('click', regenerate);
  document.querySelector('#save')?.addEventListener('click', saveTimeline);
  document.querySelector('#discover-devices')?.addEventListener('click', discoverDevices);
  document.querySelector('#style')?.addEventListener('change', () => {
    const selectedStyle = inputValue('style', state.session?.style ?? state.styles[0] ?? 'synthwave');
    if (state.session) {
      state.session.style = selectedStyle;
    }
    handleStyleChanged(selectedStyle);
    render();
  });
  document.querySelector('#toggle-sidebar')?.addEventListener('click', () => {
    state.sidebarOpen = !state.sidebarOpen;
    render();
  });
  document.querySelector('#play-toggle')?.addEventListener('pointerdown', (event) => {
    event.preventDefault();
    void togglePlayback();
  });
  document.querySelector('#stop')?.addEventListener('pointerdown', (event) => {
    event.preventDefault();
    stopPlayback();
  });
  document.querySelector('#zoom-out')?.addEventListener('click', () => setZoom(state.zoomPxPerSecond - 4));
  document.querySelector('#zoom-in')?.addEventListener('click', () => setZoom(state.zoomPxPerSecond + 4));
  document.querySelector('#timeline-zoom')?.addEventListener('input', (event) => {
    setZoom(Number((event.target as HTMLInputElement).value));
  });
  document.querySelector('#toggle-inspector')?.addEventListener('click', () => {
    state.inspectorOpen = !state.inspectorOpen;
    render();
  });
  document.querySelector('#master-brightness')?.addEventListener('input', (event) => {
    const percent = Number((event.target as HTMLInputElement).value);
    state.masterBrightness = percent;
    const readout = document.querySelector<HTMLElement>('.brightness-value');
    if (readout) {
      readout.textContent = `${Math.round(percent)}%`;
    }
    void SetMasterBrightness(percent);
  });
  document.querySelector('#tour-next')?.addEventListener('click', advanceTour);
  document.querySelector('#tour-skip')?.addEventListener('click', endTour);
  document.querySelector('#prompt-cancel')?.addEventListener('click', () => {
    state.regenerationPrompt = false;
    render();
  });
  document.querySelector('#prompt-regenerate')?.addEventListener('click', () => {
    state.regenerationPrompt = false;
    void regenerate();
  });
  bindInspectorResize();

  document.querySelectorAll<HTMLButtonElement>('[data-target-token]').forEach((button) => {
    button.addEventListener('click', () => {
      toggleTargetToken(button.dataset.targetToken ?? 'all');
      handleTargetChanged();
      render();
    });
  });

  document.querySelectorAll<HTMLButtonElement>('.device[data-device]').forEach((button) => {
    button.addEventListener('click', () => {
      toggleTargetToken(button.dataset.device ?? 'all');
      handleTargetChanged();
      render();
    });
  });

  document.querySelectorAll<HTMLButtonElement>('.event').forEach((button) => {
    button.addEventListener('click', () => {
      const index = Number(button.dataset.event ?? -1);
      if (state.selectedEvent === index) {
        state.selectedEvent = -1;
        state.inspectorOpen = false;
      } else {
        state.selectedEvent = index;
        state.inspectorOpen = true;
      }
      render();
    });
    button.addEventListener('dragstart', (event) => {
      event.dataTransfer?.setData('text/plain', button.dataset.event ?? '-1');
    });
  });

  document.querySelectorAll<HTMLElement>('.lane-events').forEach((lane) => {
    lane.addEventListener('dragover', (event) => event.preventDefault());
    lane.addEventListener('drop', (event) => {
      event.preventDefault();
      const index = Number(event.dataTransfer?.getData('text/plain') ?? -1);
      const session = state.session;
      if (!session || index < 0) {
        return;
      }
      const rect = lane.getBoundingClientRect();
      const x = clamp(event.clientX - rect.left, 0, rect.width);
      const movedEvent = session.timeline.events[index];
      movedEvent.time_ms = Math.round(x / state.zoomPxPerSecond * 1000);
      state.inspectorOpen = true;
      state.status = `Moved event to ${formatTime(movedEvent.time_ms)}`;
      sortTimeline(session.timeline);
      state.selectedEvent = session.timeline.events.indexOf(movedEvent);
      render();
    });
  });

  bindInspector();
}

function bindInspector() {
  const event = selectedEvent();
  const session = state.session;
  if (!event || !session) {
    return;
  }
  const update = () => {
    const params = mutableParams(event);
    event.time_ms = clamp(Number(inputValue('event-time', String(event.time_ms))), 0, session.timeline.duration_ms);
    const color = {
      hue: Number(inputValue('event-hue', String(colorParam(params, 'hue', 240)))),
      saturation: Number(inputValue('event-saturation', String(percentParam(params, 'saturation', 100)))),
      brightness: clamp(Number(inputValue('event-brightness', String(percentParam(params, 'brightness', 70)))), 1, 100),
      kelvin: Number(inputValue('event-kelvin', String(colorParam(params, 'kelvin', 3500)))),
    };
    applyEventColorParams(params, color);
    params.duration_ms = Number(inputValue('event-duration', String(durationParam(params, 180))));
    state.status = 'Edited selected event';
    sortTimeline(session.timeline);
    state.selectedEvent = session.timeline.events.indexOf(event);
    state.inspectorOpen = true;
    render();
  };

  ['event-time', 'event-hue', 'event-saturation', 'event-brightness', 'event-kelvin', 'event-duration']
    .forEach((id) => document.querySelector(`#${id}`)?.addEventListener('change', update));

  document.querySelector('#delete-event')?.addEventListener('click', () => {
    if (state.selectedEvent >= 0) {
      session.timeline.events.splice(state.selectedEvent, 1);
      state.selectedEvent = -1;
      state.inspectorOpen = false;
      state.status = 'Deleted event';
      render();
    }
  });

  document.querySelector('#duplicate-event')?.addEventListener('click', () => {
    if (state.selectedEvent >= 0) {
      const copy = structuredClone(session.timeline.events[state.selectedEvent]);
      copy.time_ms = Math.min(copy.time_ms + 250, session.timeline.duration_ms);
      session.timeline.events.push(copy);
      state.status = 'Duplicated event';
      sortTimeline(session.timeline);
      state.selectedEvent = session.timeline.events.indexOf(copy);
      state.inspectorOpen = true;
      render();
    }
  });
}

async function chooseSong() {
  try {
    const path = await ChooseAudioFile();
    if (!path) {
      return;
    }
    stopPlayback(false);
    state.selectedAudioPath = path;
    clearRegenerationReasons();
    state.regenerationPrompt = false;
    state.generatedStyle = '';
    state.status = `Selected ${fileName(path)}; press Generate to analyze`;
    state.session = emptySession(path);
    try {
      const durationMS = await AudioDuration(path);
      state.session.timeline.duration_ms = durationMS;
      state.session.analysis.duration_ms = durationMS;
      state.session.summary.duration_ms = durationMS;
    } catch (error) {
      state.status = `Selected ${fileName(path)}; duration unavailable: ${readableError(error)}`;
    }
    state.targetTokens = splitTarget(state.session.target);
    render();
  } catch (error) {
    state.status = readableError(error);
    render();
  }
}

async function regenerate() {
  const path = selectedSongPath();
  if (!path) {
    return;
  }
  await generateForPath(path);
}

async function discoverDevices() {
  state.loading = true;
  state.status = 'Discovering LIFX LAN devices';
  render();
  try {
    const discovered = await DiscoverDevices() as unknown as DeviceInfo[];
    state.devices = discovered;
    if (state.session) {
      state.session.devices = discovered;
    }
    if (state.targetTokens.length === 0) {
      state.targetTokens = ['all'];
    }
    syncSessionTarget();
    if (state.session?.source === 'generated') {
      markRegenerationRequired('devices', `Discovered ${discovered.length} devices; regenerate to update choreography`);
      state.status = `Discovered ${discovered.length} devices; regenerate to update choreography`;
    } else {
      state.status = `Discovered ${discovered.length} LIFX devices`;
    }
  } catch (error) {
    state.status = readableError(error);
  } finally {
    state.loading = false;
    render();
  }
}

async function generateForPath(path: string) {
  const style = inputValue('style', state.session?.style ?? 'synthwave');
  const target = targetString();
  stopPlayback(false);
  state.loading = true;
  const existingAnalysis = analysisForPath(path);
  state.status = existingAnalysis ? 'Generating timeline' : 'Analyzing song';
  render();
  try {
    const songAnalysis = existingAnalysis ?? await Analyze(path);
    state.status = 'Generating timeline';
    render();
    state.session = await GenerateFromAnalysis(path, songAnalysis as any, style, target, (state.session?.devices ?? state.devices) as any) as unknown as EditorSession;
    state.devices = state.session.devices;
    state.selectedAudioPath = path;
    state.targetTokens = splitTarget(state.session.target);
    state.selectedEvent = -1;
    state.selectedDevice = firstDeviceID(state.session);
    state.playheadMS = 0;
    clearRegenerationReasons();
    state.generatedStyle = state.session.style;
    state.regenerationPrompt = false;
    state.status = 'Generated editable timeline';
  } catch (error) {
    state.status = readableError(error);
  } finally {
    state.loading = false;
    render();
  }
}

async function saveTimeline() {
  const session = state.session;
  if (!session) {
    return;
  }
  try {
    const path = await ChooseTimelineSavePath(`${session.timeline.name || 'timeline'}.json`);
    if (!path) {
      return;
    }
    await SaveTimeline({ path, timeline: session.timeline } as any);
    state.status = `Saved ${path}`;
  } catch (error) {
    state.status = readableError(error);
  }
  render();
}

async function togglePlayback() {
  const session = state.session;
  if (!session) {
    return;
  }
  if (state.playing) {
    await pausePlayback();
    return;
  }
  if (state.previewPaused) {
    await resumePlayback();
    return;
  }

  const generated = isGeneratedTimeline(session);
  if (generated && state.needsRegeneration) {
    state.playing = false;
    state.previewStarting = false;
    state.regenerationPrompt = true;
    state.status = regenerationMessage();
    render();
    return;
  }
  state.playing = true;
  state.previewPaused = false;
  state.previewStarting = true;
  state.status = generated ? 'Starting audio and lights' : 'Starting audio preview';
  render();
  if (generated) {
    try {
      await StartPreview({
        audio_path: selectedSongPath(),
        target: targetString(),
        timeline: timelineForSelectedTargets(session),
      } as any);
    } catch (error) {
      state.playing = false;
      state.previewPaused = false;
      state.previewStarting = false;
      state.status = readableError(error);
      render();
      return;
    }
  } else {
    try {
      await StartAudioPreview(selectedSongPath());
    } catch (error) {
      state.playing = false;
      state.previewPaused = false;
      state.previewStarting = false;
      state.status = readableError(error);
      render();
      return;
    }
  }
  state.previewStarting = false;
  state.status = generated ? 'Playing generated audio and lights' : 'Previewing selected audio';

  startPlaybackTimer(session);
  render();
}

function stopPlayback(shouldRender = true) {
  state.playing = false;
  state.previewPaused = false;
  state.previewStarting = false;
  state.playheadMS = 0;
  if (playTimer !== undefined) {
    window.clearInterval(playTimer);
    playTimer = undefined;
  }
  void StopPreview();
  state.status = 'Preview stopped';
  if (shouldRender) {
    updateTransport();
  }
}

async function pausePlayback() {
  state.playing = false;
  state.previewPaused = true;
  state.previewStarting = false;
  if (playTimer !== undefined) {
    window.clearInterval(playTimer);
    playTimer = undefined;
  }
  state.status = 'Pausing preview';
  updateTransport();
  try {
    await PausePreview();
    state.status = 'Preview paused';
  } catch (error) {
    state.previewPaused = false;
    state.status = readableError(error);
  }
  updateTransport();
}

async function resumePlayback() {
  state.playing = true;
  state.status = 'Resuming preview';
  updateTransport();
  try {
    await ResumePreview();
  } catch (error) {
    state.playing = false;
    state.previewPaused = false;
    state.previewStarting = false;
    state.status = readableError(error);
    render();
    return;
  }
  state.previewPaused = false;
  state.previewStarting = false;
  const session = state.session;
  state.status = session && isGeneratedTimeline(session) ? 'Playing generated audio and lights' : 'Previewing selected audio';
  if (session) {
    startPlaybackTimer(session);
  }
  updateTransport();
}

function startPlaybackTimer(session: EditorSession) {
  if (playTimer !== undefined) {
    window.clearInterval(playTimer);
  }
  playbackStartedAt = performance.now() - state.playheadMS;
  playTimer = window.setInterval(() => {
    if (!state.playing) {
      return;
    }
    const durationMS = playbackDurationMS(session);
    const elapsed = performance.now() - playbackStartedAt;
    state.playheadMS = durationMS > 0 ? Math.min(elapsed, durationMS) : elapsed;
    if (durationMS > 0 && state.playheadMS >= durationMS) {
      stopPlayback();
      return;
    }
    updateTransport();
  }, 50);
}

function updateTransport() {
  const playButton = document.querySelector<HTMLButtonElement>('#play-toggle');
  if (playButton) {
    playButton.textContent = state.previewStarting ? 'Starting' : state.playing ? 'Pause' : 'Play';
  }
  const timecode = document.querySelector<HTMLElement>('.timecode');
  if (timecode) {
    timecode.textContent = `${formatTime(state.playheadMS)} / ${formatTime(state.session ? playbackDurationMS(state.session) : 0)}`;
  }
  const playhead = document.querySelector<HTMLElement>('.playhead');
  if (playhead) {
    playhead.style.left = `${timeToX(state.playheadMS)}px`;
  }
}

function selectedEvent() {
  const session = state.session;
  if (!session || state.selectedEvent < 0) {
    return null;
  }
  return session.timeline.events[state.selectedEvent] ?? null;
}

function isGeneratedTimeline(session: EditorSession) {
  return session.source === 'generated' && session.timeline.events.length > 0 && selectedSongPath() !== '';
}

function playbackDurationMS(session: EditorSession) {
  if (session.timeline.duration_ms > 0) {
    return session.timeline.duration_ms;
  }
  return 0;
}

function emptySession(path: string): EditorSession {
  const name = fileName(path);
  const target = targetString();
  return {
    song_path: path,
    song_name: name,
    style: inputValue('style', state.styles[0] ?? 'synthwave'),
    target,
    analysis: {
      duration_ms: 0,
      bpm: 0,
      beats: [],
      energy: [],
      sections: [],
    },
    timeline: {
      name,
      duration_ms: 0,
      events: [],
    },
    devices: state.devices,
    summary: {
      bpm: 0,
      duration_ms: 0,
      beats: 0,
      sections: 0,
      events: 0,
    },
    source: 'selected',
    event_stats: {},
  };
}

function analysisForPath(path: string) {
  const session = state.session;
  if (!session || session.song_path !== path) {
    return null;
  }
  if (session.analysis.duration_ms <= 0 || session.analysis.beats.length === 0) {
    return null;
  }
  return session.analysis;
}

function mutableParams(event: TimelineEvent) {
  if (!event.params) {
    event.params = {};
  }
  return event.params;
}

function firstDeviceID(session: EditorSession | null) {
  return session?.devices[0]?.id ?? 'all';
}

function laneIDs(session: EditorSession) {
  return selectedTargetDevices(session).map((device) => device.id);
}

function eventAppliesToLane(event: TimelineEvent, device: DeviceInfo | undefined, lane: string) {
  if (!device) {
    return event.target === lane;
  }
  const targets = splitTarget(event.target);
  return targets.some((target) => (
    sameToken(target, 'all') ||
    sameToken(target, device.id) ||
    sameToken(target, device.label) ||
    sameToken(target, device.group) ||
    sameToken(target, device.location)
  ));
}

function eventTargetLabel(session: EditorSession, event: TimelineEvent) {
  const labels = splitTarget(event.target).map((target) => {
    if (sameToken(target, 'all')) {
      return 'All targets';
    }
    const device = session.devices.find((candidate) => (
      sameToken(candidate.id, target) ||
      sameToken(candidate.label, target)
    ));
    if (device) {
      return device.label || device.id;
    }
    const groupMatch = session.devices.find((candidate) => sameToken(candidate.group, target));
    if (groupMatch) {
      return `Group: ${groupMatch.group}`;
    }
    const locationMatch = session.devices.find((candidate) => sameToken(candidate.location, target));
    if (locationMatch) {
      return `Location: ${locationMatch.location}`;
    }
    return target;
  });
  return labels.join(', ');
}

function handleTargetChanged() {
  syncSessionTarget();
  state.selectedEvent = -1;
  state.inspectorOpen = false;
  const session = state.session;
  if (!session || session.source !== 'generated') {
    return;
  }
  if (generatedTimelineCoversSelectedTargets(session)) {
    state.regenerationReasons.target = false;
    updateNeedsRegeneration();
    state.status = 'Target changed; existing choreography filtered to selected devices';
    return;
  }
  markRegenerationRequired('target', 'Target changed; regenerate to create device actions');
}

function selectedTargetDevices(session: EditorSession) {
  const tokens = state.targetTokens.length > 0 ? state.targetTokens : splitTarget(session.target || 'all');
  const seen = new Set<string>();
  const devices: DeviceInfo[] = [];
  const addDevice = (device: DeviceInfo) => {
    if (!device.id || seen.has(device.id)) {
      return;
    }
    seen.add(device.id);
    devices.push(device);
  };

  for (const token of tokens) {
    if (sameToken(token, 'all')) {
      session.devices.forEach(addDevice);
      continue;
    }
    session.devices
      .filter((device) => matchesDeviceToken(token, device))
      .forEach(addDevice);
  }
  return devices;
}

function timelineForSelectedTargets(session: EditorSession): Timeline {
  const devices = selectedTargetDevices(session);
  if (devices.length === 0) {
    return session.timeline;
  }
  const target = targetString();
  const events = session.timeline.events
    .filter((event) => eventAppliesToAnyDevice(event, devices))
    .map((event) => {
      if (splitTarget(event.target).some((token) => sameToken(token, 'all'))) {
        return { ...structuredClone(event), target };
      }
      return structuredClone(event);
    });
  return { ...session.timeline, events };
}

function visibleTimelineDurationMS(session: EditorSession) {
  const timeline = timelineForSelectedTargets(session);
  return timeline.events.reduce((maxEnd, event) => {
    return Math.max(maxEnd, event.time_ms + eventDurationMS(event));
  }, session.timeline.duration_ms);
}

function generatedTimelineCoversSelectedTargets(session: EditorSession) {
  const devices = selectedTargetDevices(session);
  if (devices.length === 0) {
    return false;
  }
  return devices.every((device) => session.timeline.events.some((event) => (
    event.action !== 'power_on' &&
    event.action !== 'power_off' &&
    eventAppliesToLane(event, device, device.id)
  )));
}

function eventAppliesToAnyDevice(event: TimelineEvent, devices: DeviceInfo[]) {
  return devices.some((device) => eventAppliesToLane(event, device, device.id));
}

function matchesDeviceToken(token: string, device: DeviceInfo) {
  return sameToken(token, device.id) ||
    sameToken(token, device.label) ||
    sameToken(token, device.group) ||
    sameToken(token, device.location);
}

function renderTicks(duration: number) {
  const step = tickStepMS();
  const ticks = [];
  for (let t = 0; t <= duration; t += step) {
    ticks.push(`<span class="time-tick" style="left:${timeToX(t)}px">${formatTime(t)}</span>`);
  }
  return ticks.join('');
}

function setZoom(value: number) {
  state.zoomPxPerSecond = clamp(value, 4, 80);
  render();
}

function timelineWidthPx(durationMS: number) {
  return Math.max(900, Math.ceil(durationMS / 1000 * state.zoomPxPerSecond));
}

function captureTimelineScroll() {
  const scroll = document.querySelector<HTMLElement>('.timeline-scroll');
  if (!scroll) {
    return;
  }
  state.timelineScrollLeft = scroll.scrollLeft;
  state.timelineScrollTop = scroll.scrollTop;
}

function restoreTimelineScroll() {
  const scroll = document.querySelector<HTMLElement>('.timeline-scroll');
  if (!scroll) {
    return;
  }
  scroll.scrollLeft = state.timelineScrollLeft;
  scroll.scrollTop = state.timelineScrollTop;
  scroll.addEventListener('scroll', () => {
    state.timelineScrollLeft = scroll.scrollLeft;
    state.timelineScrollTop = scroll.scrollTop;
  }, { passive: true });
}

function timeToX(ms: number) {
  return Math.round(ms / 1000 * state.zoomPxPerSecond);
}

function tickStepMS() {
  if (state.zoomPxPerSecond >= 56) {
    return 1000;
  }
  if (state.zoomPxPerSecond >= 28) {
    return 5000;
  }
  if (state.zoomPxPerSecond >= 10) {
    return 10000;
  }
  return 30000;
}

function selectedSongPath() {
  return state.selectedAudioPath || (state.session?.source === 'generated' ? state.session.song_path : '');
}

function targetString() {
  if (state.targetTokens.length > 0) {
    return state.targetTokens.join(',');
  }
  return state.session?.target || 'all';
}

function splitTarget(value: string) {
  return unique(value.split(',').map((part) => part.trim()).filter(Boolean));
}

function targetIncludes(value: string) {
  return state.targetTokens.some((target) => sameToken(target, value));
}

function toggleTargetToken(value: string) {
  if (!value) {
    return;
  }
  if (sameToken(value, 'all')) {
    state.targetTokens = ['all'];
    return;
  }
  const withoutAll = state.targetTokens.filter((target) => !sameToken(target, 'all'));
  if (withoutAll.some((target) => sameToken(target, value))) {
    state.targetTokens = withoutAll.filter((target) => !sameToken(target, value));
  } else {
    state.targetTokens = [...withoutAll, value];
  }
  if (state.targetTokens.length === 0) {
    state.targetTokens = ['all'];
  }
}

function syncSessionTarget() {
  if (state.session) {
    state.session.target = targetString();
  }
}

function handleStyleChanged(selectedStyle: string) {
  if (state.session?.source !== 'generated') {
    return;
  }
  if (selectedStyle === state.generatedStyle) {
    state.regenerationReasons.style = false;
    updateNeedsRegeneration();
    state.status = 'Style restored to generated timeline';
    return;
  }
  markRegenerationRequired('style', 'Style changed; regenerate to update choreography');
}

function markRegenerationRequired(reason: keyof AppState['regenerationReasons'], message: string) {
  if (state.session?.source !== 'generated') {
    return;
  }
  state.regenerationReasons[reason] = true;
  updateNeedsRegeneration();
  state.status = message;
}

function clearRegenerationReasons() {
  state.regenerationReasons = {
    style: false,
    target: false,
    devices: false,
  };
  updateNeedsRegeneration();
}

function updateNeedsRegeneration() {
  state.needsRegeneration = Object.values(state.regenerationReasons).some(Boolean);
}

function regenerationMessage() {
  if (state.regenerationReasons.style) {
    return 'The selected style needs timeline regeneration before playing lights.';
  }
  if (state.regenerationReasons.target) {
    return 'The selected target is not covered by the current timeline. Regenerate to create device actions.';
  }
  if (state.regenerationReasons.devices) {
    return 'Device discovery changed. Regenerate to update the choreography.';
  }
  return 'Regenerate the timeline before playing lights.';
}

function sameToken(a: string, b: string) {
  return a.trim().toLowerCase() === b.trim().toLowerCase();
}

function unique(values: string[]) {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const value of values) {
    const key = value.trim().toLowerCase();
    if (!key || seen.has(key)) {
      continue;
    }
    seen.add(key);
    result.push(value.trim());
  }
  return result;
}

function overviewSubtitle(session: EditorSession) {
  if (session.source === 'selected') {
    return 'Song loaded; timeline empty';
  }
  return `Style: ${session.style}`;
}

function fileName(path: string) {
  return path.split(/[\\/]/).filter(Boolean).pop() ?? path;
}

function bindInspectorResize() {
  const handle = document.querySelector<HTMLElement>('#inspector-resize');
  if (!handle) {
    return;
  }
  handle.addEventListener('pointerdown', (event) => {
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = state.inspectorWidth;
    const move = (moveEvent: PointerEvent) => {
      state.inspectorWidth = clamp(startWidth + startX - moveEvent.clientX, 220, 520);
      document.querySelector<HTMLElement>('.shell')?.style.setProperty('--inspector-width', `${state.inspectorWidth}px`);
    };
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      render();
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  });
}

function eventColor(event: TimelineEvent) {
  const params = event.params ?? {};
  const hue = colorParam(params, 'hue', 0);
  const saturation = percentParam(params, 'saturation', 0);
  const brightness = percentParam(params, 'brightness', 80);
  if (event.action === 'power_on') {
    return '#f3e7c8';
  }
  return hsla(hue, saturation, brightness);
}

function eventBackgroundStyle(event: TimelineEvent) {
  const colors = eventColors(event);
  if (colors.length <= 1) {
    const color = colors[0] ?? eventColor(event);
    return `--event-color:${color};--event-bg:${color};`;
  }
  const stops = colors
    .slice(0, 8)
    .map((color, index, list) => `${color} ${Math.round((index / Math.max(list.length - 1, 1)) * 100)}%`)
    .join(',');
  return `--event-color:${colors[0]};--event-bg:linear-gradient(90deg,${stops});`;
}

function eventColors(event: TimelineEvent) {
  if (event.action === 'power_on') {
    return ['#f3e7c8'];
  }
  const params = event.params ?? {};
  const zones = arrayParam(params, 'zones') as Array<Record<string, unknown>>;
  if (zones?.length) {
    return uniqueColorStops(zones.map((zone) => colorRecordToHsla(recordParam(zone, 'color'))));
  }
  const pixels = arrayParam(params, 'pixels') as Array<Record<string, unknown>>;
  if (pixels?.length) {
    return uniqueColorStops(pixels.map((pixel) => colorRecordToHsla(recordParam(pixel, 'color'))));
  }
  return [eventColor(event)];
}

function spatialEventSummary(event: TimelineEvent) {
  const params = event.params ?? {};
  const zones = arrayParam(params, 'zones') as Array<Record<string, unknown>>;
  if (zones.length) {
    const colors = eventColors(event);
    return {
      title: 'Zone gradient',
      detail: `${zones.length} zones`,
      colors: `${colors.length} ${colors.length === 1 ? 'color' : 'colors'}`,
    };
  }

  const pixels = arrayParam(params, 'pixels') as Array<Record<string, unknown>>;
  if (pixels.length) {
    const width = Number(paramValue(params, 'width') ?? 0);
    const height = Number(paramValue(params, 'height') ?? 0);
    const colors = eventColors(event);
    const dimensions = width > 0 && height > 0 ? `${width}x${height} matrix` : 'matrix frame';
    return {
      title: 'Matrix frame',
      detail: `${dimensions}, ${pixels.length} pixels`,
      colors: `${colors.length} ${colors.length === 1 ? 'color' : 'colors'}`,
    };
  }

  return undefined;
}

function colorRecordToHsla(color: Record<string, unknown> | undefined) {
  if (!color) {
    return hsla(0, 0, 80);
  }
  return hsla(colorRecordNumber(color, 'hue', 0), percentValue(colorRecordNumber(color, 'saturation', 0)), percentValue(colorRecordNumber(color, 'brightness', 80)));
}

function uniqueColorStops(colors: string[]) {
  const stops: string[] = [];
  for (const color of colors) {
    if (stops[stops.length - 1] !== color) {
      stops.push(color);
    }
    if (stops.length >= 8) {
      break;
    }
  }
  return stops.length > 0 ? stops : [hsla(0, 0, 80)];
}

function eventDurationMS(event: TimelineEvent) {
  const params = event.params ?? {};
  return Math.max(0, durationParam(params, 0));
}

function colorFromNested(params: Record<string, unknown>, key: string) {
  const zones = arrayParam(params, 'zones') as Array<Record<string, unknown>>;
  const zoneColor = zones[0] ? recordParam(zones[0], 'color') : undefined;
  if (zoneColor && paramValue(zoneColor, key) !== undefined) {
    return paramValue(zoneColor, key);
  }
  const pixels = arrayParam(params, 'pixels') as Array<Record<string, unknown>>;
  const pixelColor = pixels[0] ? recordParam(pixels[0], 'color') : undefined;
  if (pixelColor) {
    return paramValue(pixelColor, key);
  }
  return undefined;
}

function colorParam(params: Record<string, unknown>, key: string, fallback: number) {
  const direct = paramValue(params, key);
  if (direct !== undefined) {
    return Number(direct);
  }
  const nested = colorFromNested(params, key);
  if (nested !== undefined) {
    return Number(nested);
  }
  return fallback;
}

function percentParam(params: Record<string, unknown>, key: string, fallback: number) {
  const value = percentValue(colorParam(params, key, fallback));
  return key === 'brightness' ? clamp(value, 1, 100) : value;
}

function durationParam(params: Record<string, unknown>, fallback: number) {
  return Number(paramValue(params, 'duration_ms') ?? paramValue(params, 'durationMS') ?? fallback);
}

function percentValue(value: number) {
  return value >= 0 && value <= 1 ? value * 100 : value;
}

function paramValue(record: Record<string, unknown>, key: string) {
  return record[key] ?? record[toCamelCase(key)] ?? record[toPascalCase(key)];
}

function colorRecordNumber(record: Record<string, unknown>, key: string, fallback: number) {
  return Number(paramValue(record, key) ?? fallback);
}

function recordParam(record: Record<string, unknown>, key: string) {
  const value = paramValue(record, key);
  return value && typeof value === 'object' ? value as Record<string, unknown> : undefined;
}

function arrayParam(record: Record<string, unknown>, key: string) {
  const value = paramValue(record, key);
  return Array.isArray(value) ? value : [];
}

function toCamelCase(key: string) {
  return key.replace(/_([a-z])/g, (_, char: string) => char.toUpperCase());
}

function toPascalCase(key: string) {
  const camel = toCamelCase(key);
  return camel.charAt(0).toUpperCase() + camel.slice(1);
}

function applyEventColorParams(params: Record<string, unknown>, color: Record<string, number>) {
  const zones = arrayParam(params, 'zones') as Array<Record<string, unknown>>;
  const pixels = arrayParam(params, 'pixels') as Array<Record<string, unknown>>;
  if (zones.length || pixels.length) {
    for (const item of [...zones, ...pixels]) {
      item.color = { ...recordParam(item, 'color'), ...color };
    }
    return;
  }
  Object.assign(params, color);
}

function shortAction(action: string) {
  switch (action) {
    case 'set_color':
      return 'color';
    case 'set_zone_colors':
      return 'zones';
    case 'set_matrix_colors':
      return 'matrix';
    case 'power_on':
      return 'on';
    default:
      return action.split('_').join(' ');
  }
}

function capabilityLabel(device: DeviceInfo) {
  switch (device.capabilities.kind) {
    case 'multi_zone':
      return `${device.capabilities.zone_count}z`;
    case 'matrix':
      return `${device.capabilities.matrix_width}x${device.capabilities.matrix_height}`;
    default:
      return 'bulb';
  }
}

function sortTimeline(timeline: Timeline) {
  timeline.events.sort((a, b) => a.time_ms - b.time_ms);
}

function inputValue(id: string, fallback: string) {
  return document.querySelector<HTMLInputElement | HTMLSelectElement>(`#${id}`)?.value ?? fallback;
}

function formatTime(ms: number) {
  const total = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  const millis = Math.floor(ms % 1000);
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}.${String(millis).padStart(3, '0')}`;
}

function formatNumber(value: number, digits: number) {
  return value.toFixed(digits);
}

function percent(value: number, total: number) {
  return clamp((value / total) * 100, 0, 100);
}

function hsla(hue: number, saturation: number, brightness: number) {
  return `hsl(${hue} ${clamp(saturation, 0, 100)}% ${clamp(brightness * 0.62, 12, 72)}%)`;
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function readableError(error: unknown) {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

function escapeHTML(value: string) {
  return value.replace(/[&<>"']/g, (char) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#039;',
  }[char] ?? char));
}

function escapeAttr(value: string) {
  return escapeHTML(value);
}

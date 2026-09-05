import './style.css';

import { Analyze, AnalyzerPreparing, AudioDuration, ChooseAudioFile, ChooseTimelineSavePath, DiscoverDevices, GenerateFromAnalysis, GenerationModes, MasterBrightness, PausePreview, ResumePreview, SaveTimeline, SetMasterBrightness, StartAudioPreview, StartPreview, StopPreview, Styles } from '../wailsjs/go/main/App';

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

type AnalysisStream = {
  id: string;
  label: string;
  energy: EnergyPoint[];
  accents: number[];
};

type StreamAssignment = {
  stream: string;
  device_ids: string[];
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
  generation: string;
  target: string;
  analysis: {
    duration_ms: number;
    bpm: number;
    beats: number[];
    energy: EnergyPoint[];
    sections?: AnalysisSection[];
    streams?: AnalysisStream[];
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
  generationModes: string[];
  generationMode: string;
  layerAssignments: Record<string, string>;
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
  // Last failure, kept until dismissed or superseded. The busy modal is the only
  // other place a message appears, and it unmounts the moment work stops, so
  // errors reported there were never actually readable.
  error: string | null;
  previewStarting: boolean;
  needsRegeneration: boolean;
  regenerationReasons: {
    song: boolean;
    style: boolean;
    generation: boolean;
    assignments: boolean;
    target: boolean;
    devices: boolean;
  };
  regenerationPrompt: boolean;
  generatedStyle: string;
  generatedGenerationMode: string;
  zoomPxPerSecond: number;
  energyZoom: number;
  energyScrollLeft: number;
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
  generationModes: ['song_wide', 'musical_layers'],
  generationMode: 'song_wide',
  layerAssignments: {},
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
  error: null,
  previewStarting: false,
  needsRegeneration: false,
  regenerationReasons: {
    song: false,
    style: false,
    generation: false,
    assignments: false,
    target: false,
    devices: false,
  },
  regenerationPrompt: false,
  generatedStyle: '',
  generatedGenerationMode: 'song_wide',
  zoomPxPerSecond: 16,
  energyZoom: 1,
  energyScrollLeft: 0,
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
    state.generationModes = await GenerationModes();
    state.status = 'Discovering LIFX LAN devices';
    render();
    state.devices = await DiscoverDevices() as unknown as DeviceInfo[];
    state.targetTokens = ['all'];
    state.status = `Discovered ${state.devices.length} LIFX devices`;
  } catch (error) {
    state.devices = [];
    state.targetTokens = ['all'];
    reportFailure(`Device discovery failed: ${readableError(error)}`);
  }
  render();
}

// reportFailure records a failure somewhere that outlives the work that caused it.
function reportFailure(error: unknown) {
  const message = readableError(error);
  state.status = message;
  state.error = message;
}

function render() {
  captureTimelineScroll();
  captureEnergyScroll();
  app.innerHTML = `
    <div class="shell" style="--inspector-width:${state.inspectorOpen ? state.inspectorWidth : 0}px;">
      ${renderToolbar()}
      <div class="workspace ${state.inspectorOpen ? '' : 'inspector-closed'} ${state.sidebarOpen ? '' : 'sidebar-closed'}">
        ${renderTargets()}
        <main class="timeline-panel">
          ${renderOverview()}
          ${renderLayerAssignments(state.session)}
          ${renderTimeline()}
          ${renderAnalysis()}
        </main>
        ${renderInspector()}
      </div>
      ${renderError()}
      ${renderOverlay()}
      ${renderTour()}
    </div>
  `;
  bindEvents();
  restoreTimelineScroll();
  restoreEnergyScroll();
  // Anchor positions come from the DOM that was just built, so they can never go
  // stale against a re-render.
  positionTour();
}

function renderToolbar() {
  const session = state.session;
  const styleOptions = state.styles
    .map((style) => `<option value="${style}" ${session?.style === style ? 'selected' : ''}>${style}</option>`)
    .join('');
  const generation = session?.generation ?? state.generationMode;
  const generationOptions = state.generationModes
    .map((mode) => `<option value="${mode}" ${generation === mode ? 'selected' : ''}>${generationLabel(mode)}</option>`)
    .join('');
  const generateLabel = state.needsRegeneration && !state.regenerationReasons.song ? 'Regenerate' : 'Generate';
  return `
    <header class="toolbar">
      <div class="brand">
        <div class="mark"></div>
        <div>
          <div class="product">lifx-maestro</div>
        </div>
      </div>
      <div class="transport">
        <button id="play-toggle" class="tool icon-action transport-button ${state.previewStarting ? 'pending' : ''}" title="${transportActionLabel()}" aria-label="${transportActionLabel()}" ${state.previewStarting || !selectedSongPath() ? 'disabled' : ''}>${transportIcon()}</button>
        <button id="stop" class="tool icon-action transport-button" title="Stop" aria-label="Stop" ${state.previewStarting || !selectedSongPath() ? 'disabled' : ''}>${iconSVG('stop')}</button>
        <div class="timecode">${formatTime(state.playheadMS)} / ${formatTime(session ? playbackDurationMS(session) : 0)}</div>
      </div>
      <div class="master-output">
        <label class="brightness-control" title="Master brightness">
          <span class="field-icon" aria-label="Master brightness">${iconSVG('sun')}</span>
          <input id="master-brightness" class="brightness-slider" type="range" min="5" max="100" step="5" value="${state.masterBrightness}" aria-label="Master brightness" />
        </label>
      </div>
      <div class="actions">
        <label class="field">
          <span>Style</span>
          <select id="style" class="select-control">${styleOptions}</select>
        </label>
        <label class="field">
          <span>Mode</span>
          <select id="generation-mode" class="select-control">${generationOptions}</select>
        </label>
        <button id="choose-song" class="tool primary" ${state.loading ? 'disabled' : ''}>Choose Song</button>
        <button id="regenerate" class="tool ${state.needsRegeneration ? 'attention' : ''}" ${state.loading || !selectedSongPath() ? 'disabled' : ''}>${generateLabel}</button>
        <button id="save" class="tool icon-action download-action" title="Download selected timeline JSON" aria-label="Download selected timeline JSON" ${!session ? 'disabled' : ''}>
          <svg class="download-icon" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M12 3v12m0 0 4-4m-4 4-4-4M5 19h14" />
          </svg>
        </button>
      </div>
    </header>
  `;
}

function transportActionLabel() {
  if (state.previewStarting) {
    return 'Starting';
  }
  if (state.playing) {
    return 'Pause';
  }
  return state.previewPaused ? 'Resume' : 'Play';
}

function transportIcon() {
  if (state.previewStarting) {
    return '<span class="button-spinner" aria-hidden="true"></span>';
  }
  return iconSVG(state.playing ? 'pause' : 'play');
}

function iconSVG(name: 'play' | 'pause' | 'stop' | 'sun') {
  const content = {
    play: '<polygon points="6 3 20 12 6 21 6 3"></polygon>',
    pause: '<rect x="6" y="4" width="4" height="16" rx="1"></rect><rect x="14" y="4" width="4" height="16" rx="1"></rect>',
    stop: '<rect x="4" y="4" width="16" height="16" rx="2"></rect>',
    sun: '<circle cx="12" cy="12" r="4"></circle><path d="M12 2v2M12 20v2M4.93 4.93l1.42 1.42M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.42-1.42M17.66 6.34l1.41-1.41"></path>',
  }[name];
  return `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">${content}</svg>`;
}

function renderError() {
  if (!state.error) {
    return '';
  }
  return `
    <div class="error-banner" role="alert">
      <span>${escapeHTML(state.error)}</span>
      <button id="dismiss-error" class="tool">Dismiss</button>
    </div>
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
  // Stand aside for prompts and preview startup. During first generation the
  // guide intentionally remains above the spinner so the wait has useful context.
  if (state.previewStarting || state.regenerationPrompt) {
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
    const selected = targetIncludesDevice(device) ? 'selected' : '';
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
      <button class="device ${targetAllSelected() ? 'selected' : ''}" data-target-token="all">
        <span class="device-toggle"></span>
        <span class="device-main">
          <strong>All targets</strong>
          <small>All matching devices</small>
        </span>
        <span class="badge">mix</span>
      </button>
      ${renderTokenGroup('Groups', groups, 'group')}
      ${renderTokenGroup('Locations', locations, 'location')}
      <div class="token-title device-title">Devices</div>
      ${deviceItems}
    </aside>
  `;
}

function renderLayerAssignments(session: EditorSession | null) {
  if ((session?.generation ?? state.generationMode) !== 'musical_layers') {
    return '';
  }
  const selected = session ? selectedTargetDevices(session) : [];
  if (selected.length === 0) {
    return '';
  }
  ensureLayerAssignments();
  return `
    <section class="layer-panel">
      <div class="layer-panel-title">Layer assignments</div>
      <div class="layer-list">
        ${STREAMS.map((stream) => `
          <div class="layer-assignment" data-layer-assignment="${stream.id}">
            <span class="layer-name">${escapeHTML(stream.label)}</span>
            <details class="layer-picker">
              <summary><span class="layer-count">${layerAssignmentCount(stream.id, selected)}</span></summary>
              <div class="layer-picker-menu">
                ${selected.map((device) => `
                  <label class="layer-device-option">
                    <input
                      class="layer-device-radio"
                      type="radio"
                      name="layer-device-${escapeAttr(device.id)}"
                      value="${stream.id}"
                      data-layer-device="${escapeAttr(device.id)}"
                      ${state.layerAssignments[device.id] === stream.id ? 'checked' : ''}
                    />
                    <span>${escapeHTML(device.label || device.id)}</span>
                  </label>
                `).join('')}
              </div>
            </details>
          </div>
        `).join('')}
      </div>
    </section>
  `;
}

function layerAssignmentCount(streamID: string, devices: DeviceInfo[]) {
  const count = devices.filter((device) => state.layerAssignments[device.id] === streamID).length;
  return `${count} ${count === 1 ? 'light' : 'lights'}`;
}

function renderOverview() {
  const session = state.session;
  if (!session) {
    return `<section class="overview empty">No timeline loaded</section>`;
  }
  const visibleEvents = visibleTimelineEventCount(session);
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

function renderTokenGroup(title: string, values: string[], kind: 'group' | 'location') {
  if (values.length === 0) {
    return '';
  }
  return `
    <div class="token-group">
      <div class="token-title">${escapeHTML(title)}</div>
      <div class="token-list">
        ${values.map((value) => `
          <button class="target-token ${targetGroupSelected(value, kind) ? 'selected' : ''}" data-target-token="${escapeAttr(value)}" data-target-kind="${kind}">${escapeHTML(value)}</button>
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
  const streams = session.analysis.streams?.length
    ? session.analysis.streams
    : [{ id: 'full', label: streamLabel('full'), energy: session.analysis.energy, accents: [] }];
  const energyPaths = streams.map((stream) => `
    <path class="energy-trace stream-${energyStreamClass(stream.id)}" d="${energyPath(stream.energy, duration)}" />
  `).join('');
  const energyLegend = STREAMS
    .filter((stream) => streams.some((available) => available.id === stream.id))
    .map((stream) => `
      <span class="energy-legend-item stream-${energyStreamClass(stream.id)}">
        <span class="energy-legend-swatch"></span>${escapeHTML(stream.label)}
      </span>
    `).join('');
  const energyTicks = renderEnergyTicks(duration);
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
        <div class="energy-head">
          <div class="panel-title">Energy</div>
          <div class="energy-zoom-control">
            <button id="energy-zoom-out" class="icon-tool" title="Zoom energy out" aria-label="Zoom energy out">-</button>
            <input id="energy-zoom" type="range" min="1" max="8" step="1" value="${state.energyZoom}" aria-label="Energy graph zoom" />
            <button id="energy-zoom-in" class="icon-tool" title="Zoom energy in" aria-label="Zoom energy in">+</button>
            <span id="energy-zoom-value">${state.energyZoom}x</span>
          </div>
        </div>
        <div class="energy-chart">
          <div class="energy-axis"><span>100%</span><span>50%</span><span>0%</span></div>
          <div class="energy-scroll">
            <div class="energy-canvas" style="width:${state.energyZoom * 100}%">
              <svg viewBox="0 0 100 100" preserveAspectRatio="none">
                <line class="energy-grid" x1="0" y1="0" x2="100" y2="0" />
                <line class="energy-grid" x1="0" y1="50" x2="100" y2="50" />
                <line class="energy-grid" x1="0" y1="100" x2="100" y2="100" />
                ${energyPaths}
              </svg>
              <div class="energy-time-axis">${energyTicks}</div>
            </div>
          </div>
        </div>
        <div class="energy-legend">${energyLegend}</div>
      </div>
      <div class="sections-list">
        <div class="panel-title">Sections</div>
        ${sections}
      </div>
    </section>
  `;
}

function energyPath(points: EnergyPoint[], duration: number) {
  return points.map((point, index) => {
    const x = percent(point.time_ms, duration);
    const y = 100 - point.value * 100;
    return `${index === 0 ? 'M' : 'L'} ${x.toFixed(2)} ${y.toFixed(2)}`;
  }).join(' ');
}

function energyStreamClass(streamID: string) {
  return ['low', 'mid', 'high', 'full'].includes(streamID) ? streamID : 'full';
}

function renderEnergyTicks(duration: number) {
  const segments = Math.max(4, Math.round(state.energyZoom * 4));
  return Array.from({ length: segments + 1 }, (_, index) => {
    const ratio = index / segments;
    const timeMS = duration * ratio;
    return `<span style="left:${(ratio * 100).toFixed(3)}%">${formatAxisTime(timeMS)}</span>`;
  }).join('');
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
  const colorMarker = colorWheelMarker(hue, saturation);
  const kelvinMarker = kelvinBarMarker(kelvin);

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
            <span>${escapeHTML(spatial.brightness)}</span>
          ` : `
            <strong>${saturation <= 0 ? `${Math.round(kelvin)} K` : `${Math.round(hue)}°`}</strong>
            <span>${saturation <= 0 ? `${Math.round(brightness)}% bri` : `${Math.round(saturation)}% sat · ${Math.round(brightness)}% bri`}</span>
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
      <div class="edit-field">
        <span>Color</span>
        <div id="color-wheel" class="color-wheel ${spatial ? 'disabled' : ''}" style="--marker-x:${colorMarker.x}%;--marker-y:${colorMarker.y}%;" role="slider" aria-label="Hue and saturation"></div>
        <input id="event-hue" type="hidden" value="${hue}" />
        <input id="event-saturation" type="hidden" value="${saturation}" />
      </div>
      <label class="edit-field">
        <span>Brightness</span>
        <input id="event-brightness" type="range" min="1" max="100" value="${brightness}" ${colorControlsDisabled} />
      </label>
      <label class="edit-field">
        <span>Kelvin</span>
        <div id="kelvin-bar" class="kelvin-bar ${spatial ? 'disabled' : ''}" style="--marker-x:${kelvinMarker}%;" role="slider" aria-label="Kelvin"></div>
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
  document.querySelector('#generation-mode')?.addEventListener('change', () => {
    const selectedMode = inputValue('generation-mode', state.generationMode);
    state.generationMode = selectedMode;
    if (state.session) {
      state.session.generation = selectedMode;
    }
    ensureLayerAssignments();
    handleGenerationChanged(selectedMode);
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
  document.querySelector('#energy-zoom-out')?.addEventListener('click', () => setEnergyZoom(state.energyZoom - 1));
  document.querySelector('#energy-zoom-in')?.addEventListener('click', () => setEnergyZoom(state.energyZoom + 1));
  document.querySelector('#energy-zoom')?.addEventListener('change', (event) => {
    setEnergyZoom(Number((event.target as HTMLInputElement).value));
  });
  document.querySelector('#toggle-inspector')?.addEventListener('click', () => {
    state.inspectorOpen = !state.inspectorOpen;
    render();
  });
  document.querySelector('#dismiss-error')?.addEventListener('click', () => {
    state.error = null;
    render();
  });
  document.querySelector('#master-brightness')?.addEventListener('input', (event) => {
    const percent = Number((event.target as HTMLInputElement).value);
    state.masterBrightness = percent;
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
      toggleTargetToken(button.dataset.targetToken ?? 'all', button.dataset.targetKind as TargetTokenKind | undefined);
      handleTargetChanged();
      render();
    });
  });

  document.querySelectorAll<HTMLButtonElement>('.device[data-device]').forEach((button) => {
    button.addEventListener('click', () => {
      toggleDeviceTarget(button.dataset.device ?? 'all');
      handleTargetChanged();
      render();
    });
  });
  document.querySelectorAll<HTMLInputElement>('.layer-device-radio[data-layer-device]').forEach((radio) => {
    radio.addEventListener('change', () => {
      const deviceID = radio.dataset.layerDevice ?? '';
      if (!deviceID || !radio.checked) {
        return;
      }
      state.layerAssignments[deviceID] = radio.value;
      handleAssignmentsChanged();
      updateRegenerationControl();
      updateLayerAssignmentCounts();
    });
  });
  const layerPickers = Array.from(document.querySelectorAll<HTMLDetailsElement>('.layer-picker'));
  layerPickers.forEach((picker) => {
    picker.addEventListener('toggle', () => {
      if (!picker.open) {
        return;
      }
      layerPickers.forEach((other) => {
        if (other !== picker) {
          other.open = false;
        }
      });
    });
  });
  document.querySelector<HTMLElement>('.shell')?.addEventListener('pointerdown', (event) => {
    if ((event.target as HTMLElement).closest('.layer-picker')) {
      return;
    }
    layerPickers.forEach((picker) => {
      picker.open = false;
    });
  });

  const timeline = document.querySelector<HTMLElement>('.timeline');
  timeline?.addEventListener('click', (event) => {
    const button = (event.target as HTMLElement).closest<HTMLButtonElement>('.event');
    if (button) {
      const index = Number(button.dataset.event ?? -1);
      if (state.selectedEvent === index) {
        state.selectedEvent = -1;
        state.inspectorOpen = false;
      } else {
        state.selectedEvent = index;
        state.inspectorOpen = true;
      }
      render();
    }
  });
  timeline?.addEventListener('dragstart', (event) => {
    const button = (event.target as HTMLElement).closest<HTMLButtonElement>('.event');
    if (button) {
      event.dataTransfer?.setData('text/plain', button.dataset.event ?? '-1');
    }
  });
  timeline?.addEventListener('dragover', (event) => {
    if ((event.target as HTMLElement).closest('.lane-events')) {
      event.preventDefault();
    }
  });
  timeline?.addEventListener('drop', (event) => {
    const lane = (event.target as HTMLElement).closest<HTMLElement>('.lane-events');
    if (!lane) {
      return;
    }
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

  bindColorWheel(update);
  bindKelvinBar(update);

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
    state.generatedGenerationMode = state.generationMode;
    state.status = `Selected ${fileName(path)}; press Generate to analyze`;
    state.session = emptySession(path);
    state.regenerationReasons.song = true;
    updateNeedsRegeneration();
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
    reportFailure(error);
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
    reportFailure(error);
  } finally {
    state.loading = false;
    render();
  }
}

async function generateForPath(path: string) {
  const style = inputValue('style', state.session?.style ?? 'synthwave');
  const generation = inputValue('generation-mode', state.generationMode);
  const target = targetString();
  stopPlayback(false);
  state.error = null;
  state.loading = true;
  const existingAnalysis = analysisForPath(path);
  state.status = existingAnalysis ? 'Generating timeline' : 'Analyzing song';
  render();
  try {
    const songAnalysis = existingAnalysis ?? await Analyze(path);
    state.status = 'Generating timeline';
    render();
    state.session = await GenerateFromAnalysis(path, songAnalysis as any, style, target, generation, streamAssignments(generation), (state.session?.devices ?? state.devices) as any) as unknown as EditorSession;
    state.devices = state.session.devices;
    state.generationMode = state.session.generation || generation;
    ensureLayerAssignments();
    state.selectedAudioPath = path;
    state.targetTokens = splitTarget(state.session.target);
    state.selectedEvent = -1;
    state.selectedDevice = firstDeviceID(state.session);
    state.playheadMS = 0;
    clearRegenerationReasons();
    state.generatedStyle = state.session.style;
    state.generatedGenerationMode = state.session.generation;
    state.regenerationPrompt = false;
    state.status = 'Generated editable timeline';
  } catch (error) {
    reportFailure(error);
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
    await SaveTimeline({ path, timeline: timelineForSelectedTargets(session) } as any);
    state.status = `Saved ${path}`;
  } catch (error) {
    reportFailure(error);
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
      reportFailure(error);
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
      reportFailure(error);
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
    reportFailure(error);
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
    reportFailure(error);
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
    const label = transportActionLabel();
    playButton.innerHTML = transportIcon();
    playButton.title = label;
    playButton.setAttribute('aria-label', label);
    playButton.classList.toggle('pending', state.previewStarting);
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
    generation: inputValue('generation-mode', state.generationMode),
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
  ensureLayerAssignments();
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

function handleGenerationChanged(selectedMode: string) {
  if (state.session?.source !== 'generated') {
    return;
  }
  if (selectedMode === state.generatedGenerationMode) {
    state.regenerationReasons.generation = false;
    state.regenerationReasons.assignments = false;
    updateNeedsRegeneration();
    if (!state.needsRegeneration) {
      state.regenerationPrompt = false;
    }
    state.status = 'Generation mode restored to generated timeline';
    return;
  }
  markRegenerationRequired('generation', 'Generation mode changed; regenerate to update choreography');
  state.regenerationPrompt = true;
}

function handleAssignmentsChanged() {
  if ((state.session?.generation ?? state.generationMode) !== 'musical_layers') {
    return;
  }
  markRegenerationRequired('assignments', 'Layer assignments changed; regenerate to update choreography');
}

function selectedTargetDevices(session: EditorSession) {
  const tokens = state.targetTokens;
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
    return { ...session.timeline, events: [] };
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
  const devices = selectedTargetDevices(session);
  if (devices.length === 0) {
    return session.timeline.duration_ms;
  }
  return session.timeline.events.reduce((maxEnd, event) => {
    if (!eventAppliesToAnyDevice(event, devices)) {
      return maxEnd;
    }
    return Math.max(maxEnd, event.time_ms + eventDurationMS(event));
  }, session.timeline.duration_ms);
}

function visibleTimelineEventCount(session: EditorSession) {
  const devices = selectedTargetDevices(session);
  if (devices.length === 0) {
    return 0;
  }
  return session.timeline.events.reduce((count, event) => (
    count + (eventAppliesToAnyDevice(event, devices) ? 1 : 0)
  ), 0);
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

function setEnergyZoom(value: number) {
  const scroll = document.querySelector<HTMLElement>('.energy-scroll');
  const centerRatio = scroll && scroll.scrollWidth > 0
    ? (scroll.scrollLeft + scroll.clientWidth / 2) / scroll.scrollWidth
    : 0.5;
  state.energyZoom = clamp(Math.round(value), 1, 8);
  const canvas = document.querySelector<HTMLElement>('.energy-canvas');
  const input = document.querySelector<HTMLInputElement>('#energy-zoom');
  const readout = document.querySelector<HTMLElement>('#energy-zoom-value');
  const axis = document.querySelector<HTMLElement>('.energy-time-axis');
  if (canvas) {
    canvas.style.width = `${state.energyZoom * 100}%`;
  }
  if (input) {
    input.value = String(state.energyZoom);
  }
  if (readout) {
    readout.textContent = `${state.energyZoom}x`;
  }
  if (axis && state.session) {
    axis.innerHTML = renderEnergyTicks(Math.max(state.session.timeline.duration_ms, 1));
  }
  if (scroll) {
    scroll.scrollLeft = Math.max(0, centerRatio * scroll.scrollWidth - scroll.clientWidth / 2);
    state.energyScrollLeft = scroll.scrollLeft;
  }
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

function captureEnergyScroll() {
  const scroll = document.querySelector<HTMLElement>('.energy-scroll');
  if (scroll) {
    state.energyScrollLeft = scroll.scrollLeft;
  }
}

function restoreEnergyScroll() {
  const scroll = document.querySelector<HTMLElement>('.energy-scroll');
  if (!scroll) {
    return;
  }
  scroll.scrollLeft = state.energyScrollLeft;
  scroll.addEventListener('scroll', () => {
    state.energyScrollLeft = scroll.scrollLeft;
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
  return state.targetTokens.join(',');
}

function splitTarget(value: string) {
  return unique(value.split(',').map((part) => part.trim()).filter(Boolean));
}

const STREAMS: AnalysisStream[] = [
  { id: 'low', label: 'Low (20-250 Hz)', energy: [], accents: [] },
  { id: 'mid', label: 'Mid (250-4,000 Hz)', energy: [], accents: [] },
  { id: 'high', label: 'High (4,000-12,000 Hz)', energy: [], accents: [] },
  { id: 'full', label: 'Full spectrum / accents', energy: [], accents: [] },
];

function generationLabel(mode: string) {
  switch (mode) {
    case 'musical_layers':
      return 'Musical layers';
    case 'song_wide':
      return 'Full mix';
    default:
      return mode.replace(/_/g, ' ');
  }
}

function streamLabel(streamID: string) {
  return STREAMS.find((stream) => stream.id === streamID)?.label ?? streamID;
}

function defaultStreamForDevice(device: DeviceInfo) {
  switch (device.capabilities.kind) {
    case 'matrix':
      return 'high';
    case 'multi_zone':
      return 'low';
    case 'single_zone':
      return 'mid';
    default:
      return 'full';
  }
}

function ensureLayerAssignments() {
  const session = state.session;
  if (!session) {
    return;
  }
  const selected = selectedTargetDevices(session);
  const selectedIDs = new Set(selected.map((device) => device.id));
  for (const device of selected) {
    if (!state.layerAssignments[device.id]) {
      state.layerAssignments[device.id] = defaultStreamForDevice(device);
    }
  }
  for (const deviceID of Object.keys(state.layerAssignments)) {
    if (!selectedIDs.has(deviceID)) {
      delete state.layerAssignments[deviceID];
    }
  }
}

function streamAssignments(mode = state.session?.generation ?? state.generationMode): StreamAssignment[] {
  const session = state.session;
  if (!session || mode !== 'musical_layers') {
    return [];
  }
  ensureLayerAssignments();
  const byStream = new Map<string, string[]>();
  for (const [deviceID, stream] of Object.entries(state.layerAssignments)) {
    if (!deviceID || !stream) {
      continue;
    }
    byStream.set(stream, [...(byStream.get(stream) ?? []), deviceID]);
  }
  return Array.from(byStream.entries()).map(([stream, device_ids]) => ({ stream, device_ids }));
}

type TargetTokenKind = 'group' | 'location';

function targetIncludes(value: string) {
  return state.targetTokens.some((target) => sameToken(target, value));
}

function targetIncludesDevice(device: DeviceInfo) {
  return state.targetTokens.some((target) => sameToken(target, 'all') || matchesDeviceToken(target, device));
}

function targetGroupSelected(value: string, kind: TargetTokenKind) {
  const session = state.session;
  const devices = session?.devices ?? state.devices;
  const matching = devices.filter((device) => targetTokenMatchesDevice(value, kind, device));
  return matching.length > 0 && matching.every((device) => targetIncludesDevice(device));
}

function targetAllSelected() {
  const session = state.session;
  const devices = session?.devices ?? state.devices;
  return devices.length > 0 && devices.every((device) => targetIncludesDevice(device));
}

function toggleTargetToken(value: string, kind?: TargetTokenKind) {
  if (!value) {
    return;
  }
  if (sameToken(value, 'all')) {
    state.targetTokens = targetAllSelected() ? [] : ['all'];
    return;
  }
  const devices = state.session?.devices ?? state.devices;
  const groupDevices = kind ? devices.filter((device) => targetTokenMatchesDevice(value, kind, device)) : [];
  const withoutAll = state.targetTokens.filter((target) => !sameToken(target, 'all'));
  if (groupDevices.length > 0 && groupDevices.every((device) => targetIncludesDevice(device))) {
    const groupDeviceIDs = new Set(groupDevices.map((device) => device.id.toLowerCase()));
    state.targetTokens = selectedDevicesFromTokens(state.targetTokens, devices)
      .filter((device) => !groupDeviceIDs.has(device.id.toLowerCase()))
      .map((device) => device.id);
  } else if (withoutAll.some((target) => sameToken(target, value))) {
    state.targetTokens = withoutAll.filter((target) => !sameToken(target, value));
  } else {
    const groupDeviceIDs = new Set(groupDevices.map((device) => device.id.toLowerCase()));
    state.targetTokens = [...withoutAll.filter((target) => !groupDeviceIDs.has(target.toLowerCase())), value];
  }
}

function targetTokenMatchesDevice(value: string, kind: TargetTokenKind, device: DeviceInfo) {
  return kind === 'group' ? sameToken(device.group, value) : sameToken(device.location, value);
}

function toggleDeviceTarget(deviceID: string) {
  const session = state.session;
  const devices = session?.devices ?? state.devices;
  const device = devices.find((candidate) => sameToken(candidate.id, deviceID));
  if (!device) {
    toggleTargetToken(deviceID);
    return;
  }
  const withoutAll = state.targetTokens.filter((target) => !sameToken(target, 'all'));
  if (targetIncludesDevice(device)) {
    const keptDevices = selectedDevicesFromTokens(state.targetTokens, devices)
      .filter((candidate) => !sameToken(candidate.id, device.id));
    state.targetTokens = keptDevices.map((candidate) => candidate.id);
  } else {
    state.targetTokens = withoutAll
      .filter((target) => !tokenBroadlyIncludesDevice(target, device))
      .concat(device.id);
  }
}

function selectedDevicesFromTokens(tokens: string[], devices: DeviceInfo[]) {
  const seen = new Set<string>();
  const selected: DeviceInfo[] = [];
  for (const token of tokens) {
    for (const device of devices) {
      if ((sameToken(token, 'all') || matchesDeviceToken(token, device)) && !seen.has(device.id)) {
        seen.add(device.id);
        selected.push(device);
      }
    }
  }
  return selected;
}

function tokenBroadlyIncludesDevice(token: string, device: DeviceInfo) {
  return sameToken(token, device.group) || sameToken(token, device.location);
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
    if (!state.needsRegeneration) {
      state.regenerationPrompt = false;
    }
    state.status = 'Style restored to generated timeline';
    return;
  }
  markRegenerationRequired('style', 'Style changed; regenerate to update choreography');
  state.regenerationPrompt = true;
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
    song: false,
    style: false,
    generation: false,
    assignments: false,
    target: false,
    devices: false,
  };
  updateNeedsRegeneration();
}

function updateNeedsRegeneration() {
  state.needsRegeneration = Object.values(state.regenerationReasons).some(Boolean);
}

function updateRegenerationControl() {
  const button = document.querySelector<HTMLButtonElement>('#regenerate');
  if (!button) {
    return;
  }
  button.classList.toggle('attention', state.needsRegeneration);
  button.textContent = state.needsRegeneration && !state.regenerationReasons.song ? 'Regenerate' : 'Generate';
}

function updateLayerAssignmentCounts() {
  const session = state.session;
  if (!session) {
    return;
  }
  const devices = selectedTargetDevices(session);
  document.querySelectorAll<HTMLElement>('[data-layer-assignment]').forEach((assignment) => {
    const count = assignment.querySelector<HTMLElement>('.layer-count');
    if (count) {
      count.textContent = layerAssignmentCount(assignment.dataset.layerAssignment ?? '', devices);
    }
  });
}

function regenerationMessage() {
  if (state.regenerationReasons.song) {
    return 'Generate a timeline for the selected song before playing lights.';
  }
  if (state.regenerationReasons.style) {
    return 'The selected style needs timeline regeneration before playing lights.';
  }
  if (state.regenerationReasons.generation) {
    return 'The selected generation mode needs timeline regeneration before playing lights.';
  }
  if (state.regenerationReasons.assignments) {
    return 'Layer assignments changed. Regenerate to update which musical layer each device follows.';
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
  const kelvin = colorParam(params, 'kelvin', 3500);
  if (event.action === 'power_on') {
    return '#f3e7c8';
  }
  if (saturation <= 0) {
    return kelvinColor(kelvin, brightness);
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
    const brightness = brightnessSummary(zones.map((zone) => recordParam(zone, 'color')));
    return {
      title: 'Zone gradient',
      detail: `${zones.length} zones`,
      colors: `${colors.length} ${colors.length === 1 ? 'color' : 'colors'}`,
      brightness,
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
      brightness: brightnessSummary(pixels.map((pixel) => recordParam(pixel, 'color'))),
    };
  }

  return undefined;
}

function brightnessSummary(colors: Array<Record<string, unknown> | undefined>) {
  const values = colors
    .map((color) => color ? percentValue(colorRecordNumber(color, 'brightness', Number.NaN)) : Number.NaN)
    .filter((value) => Number.isFinite(value))
    .map((value) => clamp(value, 1, 100));
  if (values.length === 0) {
    return 'Brightness unavailable';
  }
  const minValue = Math.min(...values);
  const maxValue = Math.max(...values);
  const average = values.reduce((sum, value) => sum + value, 0) / values.length;
  if (Math.round(minValue) === Math.round(maxValue)) {
    return `${Math.round(average)}% bri`;
  }
  return `${Math.round(minValue)}-${Math.round(maxValue)}% bri`;
}

function colorWheelMarker(hue: number, saturation: number) {
  const angle = positiveModulo(hue + 180, 360) * Math.PI / 180;
  const radius = clamp(saturation, 0, 100) / 2;
  return {
    x: 50 + Math.cos(angle) * radius,
    y: 50 + Math.sin(angle) * radius,
  };
}

function kelvinBarMarker(kelvin: number) {
  return clamp((kelvin - 1500) / (9000 - 1500) * 100, 0, 100);
}

function bindColorWheel(update: () => void) {
  const wheel = document.querySelector<HTMLElement>('#color-wheel');
  if (!wheel || wheel.classList.contains('disabled')) {
    return;
  }
  const setFromPointer = (event: PointerEvent) => {
    const rect = wheel.getBoundingClientRect();
    const x = event.clientX - rect.left - rect.width / 2;
    const y = event.clientY - rect.top - rect.height / 2;
    const radius = Math.min(rect.width, rect.height) / 2;
    const distance = Math.min(Math.hypot(x, y), radius);
    const hue = positiveModulo(Math.round(Math.atan2(y, x) * 180 / Math.PI + 180), 360);
    const saturation = Math.round(distance / radius * 100);
    setInputValue('event-hue', String(hue));
    setInputValue('event-saturation', String(saturation));
    update();
  };
  wheel.addEventListener('pointerdown', (event) => {
    event.preventDefault();
    wheel.setPointerCapture(event.pointerId);
    setFromPointer(event);
  });
  wheel.addEventListener('pointermove', (event) => {
    if (event.buttons === 1) {
      setFromPointer(event);
    }
  });
}

function bindKelvinBar(update: () => void) {
  const bar = document.querySelector<HTMLElement>('#kelvin-bar');
  if (!bar || bar.classList.contains('disabled')) {
    return;
  }
  const setFromPointer = (event: PointerEvent) => {
    const rect = bar.getBoundingClientRect();
    const percent = clamp((event.clientX - rect.left) / rect.width, 0, 1);
    const kelvin = Math.round((1500 + percent * (9000 - 1500)) / 100) * 100;
    setInputValue('event-kelvin', String(kelvin));
    setInputValue('event-saturation', '0');
    update();
  };
  bar.addEventListener('pointerdown', (event) => {
    event.preventDefault();
    bar.setPointerCapture(event.pointerId);
    setFromPointer(event);
  });
  bar.addEventListener('pointermove', (event) => {
    if (event.buttons === 1) {
      setFromPointer(event);
    }
  });
}

function colorRecordToHsla(color: Record<string, unknown> | undefined) {
  if (!color) {
    return hsla(0, 0, 80);
  }
  const saturation = percentValue(colorRecordNumber(color, 'saturation', 0));
  const brightness = percentValue(colorRecordNumber(color, 'brightness', 80));
  if (saturation <= 0) {
    return kelvinColor(colorRecordNumber(color, 'kelvin', 3500), brightness);
  }
  return hsla(colorRecordNumber(color, 'hue', 0), saturation, brightness);
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

function setInputValue(id: string, value: string) {
  const input = document.querySelector<HTMLInputElement | HTMLSelectElement>(`#${id}`);
  if (input) {
    input.value = value;
  }
}

function formatTime(ms: number) {
  const total = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  const millis = Math.floor(ms % 1000);
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}.${String(millis).padStart(3, '0')}`;
}

function formatAxisTime(ms: number) {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
  }
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
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

function kelvinColor(kelvin: number, brightness: number) {
  const [red, green, blue] = kelvinRgb(kelvin);
  const displayLightness = clamp(brightness / 100, 0, 1);
  const scale = 0.42 + displayLightness * 0.82;
  return `rgb(${clampRgb(red * scale)} ${clampRgb(green * scale)} ${clampRgb(blue * scale)})`;
}

function kelvinRgb(kelvin: number): [number, number, number] {
  const temperature = clamp(kelvin / 100, 10, 400);
  let red: number;
  let green: number;
  let blue: number;

  if (temperature <= 66) {
    red = 255;
    green = 99.4708025861 * Math.log(temperature) - 161.1195681661;
    blue = temperature <= 19 ? 0 : 138.5177312231 * Math.log(temperature - 10) - 305.0447927307;
  } else {
    red = 329.698727446 * Math.pow(temperature - 60, -0.1332047592);
    green = 288.1221695283 * Math.pow(temperature - 60, -0.0755148492);
    blue = 255;
  }

  return [softenWhite(red), softenWhite(green), softenWhite(blue)];
}

function softenWhite(channel: number) {
  return clampRgb(channel * 0.62 + 255 * 0.38);
}

function clampRgb(channel: number) {
  return Math.round(clamp(channel, 0, 255));
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function positiveModulo(value: number, modulo: number) {
  return ((value % modulo) + modulo) % modulo;
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

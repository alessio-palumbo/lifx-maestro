export namespace analysis {
	
	export class EnergyPoint {
	    time_ms: number;
	    value: number;
	
	    static createFrom(source: any = {}) {
	        return new EnergyPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time_ms = source["time_ms"];
	        this.value = source["value"];
	    }
	}
	export class Section {
	    start_ms: number;
	    end_ms: number;
	    type: string;
	    energy: number;
	
	    static createFrom(source: any = {}) {
	        return new Section(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start_ms = source["start_ms"];
	        this.end_ms = source["end_ms"];
	        this.type = source["type"];
	        this.energy = source["energy"];
	    }
	}
	export class SongAnalysis {
	    duration_ms: number;
	    bpm: number;
	    beats: number[];
	    energy: EnergyPoint[];
	    sections?: Section[];
	
	    static createFrom(source: any = {}) {
	        return new SongAnalysis(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.duration_ms = source["duration_ms"];
	        this.bpm = source["bpm"];
	        this.beats = source["beats"];
	        this.energy = this.convertValues(source["energy"], EnergyPoint);
	        this.sections = this.convertValues(source["sections"], Section);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class EditorDeviceCapabilities {
	    kind: string;
	    has_color: boolean;
	    has_kelvin: boolean;
	    zone_count: number;
	    matrix_width: number;
	    matrix_height: number;
	    matrix_length: number;
	
	    static createFrom(source: any = {}) {
	        return new EditorDeviceCapabilities(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.has_color = source["has_color"];
	        this.has_kelvin = source["has_kelvin"];
	        this.zone_count = source["zone_count"];
	        this.matrix_width = source["matrix_width"];
	        this.matrix_height = source["matrix_height"];
	        this.matrix_length = source["matrix_length"];
	    }
	}
	export class EditorDevice {
	    id: string;
	    label: string;
	    group: string;
	    location: string;
	    capabilities: EditorDeviceCapabilities;
	
	    static createFrom(source: any = {}) {
	        return new EditorDevice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.group = source["group"];
	        this.location = source["location"];
	        this.capabilities = this.convertValues(source["capabilities"], EditorDeviceCapabilities);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class EditorEvent {
	    time_ms: number;
	    target: string;
	    action: string;
	    params?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new EditorEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time_ms = source["time_ms"];
	        this.target = source["target"];
	        this.action = source["action"];
	        this.params = source["params"];
	    }
	}
	export class EditorSummary {
	    bpm: number;
	    duration_ms: number;
	    beats: number;
	    sections: number;
	    events: number;
	
	    static createFrom(source: any = {}) {
	        return new EditorSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bpm = source["bpm"];
	        this.duration_ms = source["duration_ms"];
	        this.beats = source["beats"];
	        this.sections = source["sections"];
	        this.events = source["events"];
	    }
	}
	export class EditorTimeline {
	    name: string;
	    duration_ms: number;
	    events: EditorEvent[];
	
	    static createFrom(source: any = {}) {
	        return new EditorTimeline(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.duration_ms = source["duration_ms"];
	        this.events = this.convertValues(source["events"], EditorEvent);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EditorSession {
	    song_path: string;
	    song_name: string;
	    style: string;
	    target: string;
	    analysis: analysis.SongAnalysis;
	    timeline: EditorTimeline;
	    devices: EditorDevice[];
	    summary: EditorSummary;
	    generated: string;
	    source: string;
	    event_stats: Record<string, number>;
	
	    static createFrom(source: any = {}) {
	        return new EditorSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.song_path = source["song_path"];
	        this.song_name = source["song_name"];
	        this.style = source["style"];
	        this.target = source["target"];
	        this.analysis = this.convertValues(source["analysis"], analysis.SongAnalysis);
	        this.timeline = this.convertValues(source["timeline"], EditorTimeline);
	        this.devices = this.convertValues(source["devices"], EditorDevice);
	        this.summary = this.convertValues(source["summary"], EditorSummary);
	        this.generated = source["generated"];
	        this.source = source["source"];
	        this.event_stats = source["event_stats"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class PreviewRequest {
	    audio_path: string;
	    target: string;
	    timeline: EditorTimeline;
	
	    static createFrom(source: any = {}) {
	        return new PreviewRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.audio_path = source["audio_path"];
	        this.target = source["target"];
	        this.timeline = this.convertValues(source["timeline"], EditorTimeline);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SaveTimelineRequest {
	    path: string;
	    timeline: EditorTimeline;
	
	    static createFrom(source: any = {}) {
	        return new SaveTimelineRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.timeline = this.convertValues(source["timeline"], EditorTimeline);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}


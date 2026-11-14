export namespace main {
	
	export class Preview {
	    id: string;
	    name: string;
	    length: number;
	    files: p2p.File[];
	    trackers: string[];
	    private: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Preview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.length = source["length"];
	        this.files = this.convertValues(source["files"], p2p.File);
	        this.trackers = source["trackers"];
	        this.private = source["private"];
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

export namespace p2p {
	
	export class File {
	    path: string;
	    length: number;
	    offset: number;
	
	    static createFrom(source: any = {}) {
	        return new File(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.length = source["length"];
	        this.offset = source["offset"];
	    }
	}
	export class Progress {
	    state: string;
	    completed: number;
	    total: number;
	    downloaded: number;
	    uploaded: number;
	    downloadSpeed: number;
	    uploadSpeed: number;
	    peers: number;
	    message: string;
	    fileCompleted: number[];
	
	    static createFrom(source: any = {}) {
	        return new Progress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.completed = source["completed"];
	        this.total = source["total"];
	        this.downloaded = source["downloaded"];
	        this.uploaded = source["uploaded"];
	        this.downloadSpeed = source["downloadSpeed"];
	        this.uploadSpeed = source["uploadSpeed"];
	        this.peers = source["peers"];
	        this.message = source["message"];
	        this.fileCompleted = source["fileCompleted"];
	    }
	}

}

export namespace queue {
	
	export class Job {
	    id: string;
	    name: string;
	    destination: string;
	    selected: number[];
	    files: p2p.File[];
	    trackers: string[];
	    seedRatio: number;
	    downloadLimit: number;
	    uploadLimit: number;
	    progress: p2p.Progress;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.destination = source["destination"];
	        this.selected = source["selected"];
	        this.files = this.convertValues(source["files"], p2p.File);
	        this.trackers = source["trackers"];
	        this.seedRatio = source["seedRatio"];
	        this.downloadLimit = source["downloadLimit"];
	        this.uploadLimit = source["uploadLimit"];
	        this.progress = this.convertValues(source["progress"], p2p.Progress);
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
	export class Snapshot {
	    revision: number;
	    job?: Job;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.revision = source["revision"];
	        this.job = this.convertValues(source["job"], Job);
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


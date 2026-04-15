export namespace app {
	
	export class GitRepo {
	    path: string;
	    name: string;
	    branch: string;
	    remote: string;
	    remoteUrl: string;
	    lastSyncTime: string;
	    status: string;
	    enabled: boolean;
	    autoSync: boolean;
	    intervalSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new GitRepo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.branch = source["branch"];
	        this.remote = source["remote"];
	        this.remoteUrl = source["remoteUrl"];
	        this.lastSyncTime = source["lastSyncTime"];
	        this.status = source["status"];
	        this.enabled = source["enabled"];
	        this.autoSync = source["autoSync"];
	        this.intervalSeconds = source["intervalSeconds"];
	    }
	}
	export class GetAutoSyncStatusRes {
	    running: boolean;
	    repos: GitRepo[];
	
	    static createFrom(source: any = {}) {
	        return new GetAutoSyncStatusRes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.repos = this.convertValues(source["repos"], GitRepo);
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
	export class GetGitRepoInfoReq {
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new GetGitRepoInfoReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	    }
	}
	export class GetGitRepoInfoRes {
	    success: boolean;
	    message: string;
	    repo?: GitRepo;
	    isGitRepo: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GetGitRepoInfoRes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.repo = this.convertValues(source["repo"], GitRepo);
	        this.isGitRepo = source["isGitRepo"];
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
	export class GetSyncLogsReq {
	    repoPath: string;
	    limit: number;
	
	    static createFrom(source: any = {}) {
	        return new GetSyncLogsReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repoPath = source["repoPath"];
	        this.limit = source["limit"];
	    }
	}
	export class GitSyncLog {
	    id: number;
	    repoName: string;
	    repoPath: string;
	    time: string;
	    success: boolean;
	    message: string;
	    commitLog: string;
	    pullLog: string;
	    pushLog: string;
	
	    static createFrom(source: any = {}) {
	        return new GitSyncLog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.repoName = source["repoName"];
	        this.repoPath = source["repoPath"];
	        this.time = source["time"];
	        this.success = source["success"];
	        this.message = source["message"];
	        this.commitLog = source["commitLog"];
	        this.pullLog = source["pullLog"];
	        this.pushLog = source["pushLog"];
	    }
	}
	export class GetSyncLogsRes {
	    success: boolean;
	    message: string;
	    logs: GitSyncLog[];
	
	    static createFrom(source: any = {}) {
	        return new GetSyncLogsRes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.logs = this.convertValues(source["logs"], GitSyncLog);
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
	
	export class GitRepoListReq {
	    repos: GitRepo[];
	
	    static createFrom(source: any = {}) {
	        return new GitRepoListReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repos = this.convertValues(source["repos"], GitRepo);
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
	export class GitRepoListRes {
	    success: boolean;
	    message: string;
	    repos: GitRepo[];
	
	    static createFrom(source: any = {}) {
	        return new GitRepoListRes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.repos = this.convertValues(source["repos"], GitRepo);
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
	
	export class GitSyncReq {
	    repos: GitRepo[];
	
	    static createFrom(source: any = {}) {
	        return new GitSyncReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repos = this.convertValues(source["repos"], GitRepo);
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
	export class GitSyncResult {
	    path: string;
	    name: string;
	    success: boolean;
	    message: string;
	    pullLog: string;
	    pushLog: string;
	    commitLog: string;
	
	    static createFrom(source: any = {}) {
	        return new GitSyncResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.success = source["success"];
	        this.message = source["message"];
	        this.pullLog = source["pullLog"];
	        this.pushLog = source["pushLog"];
	        this.commitLog = source["commitLog"];
	    }
	}
	export class GitSyncRes {
	    success: boolean;
	    message: string;
	    results: GitSyncResult[];
	
	    static createFrom(source: any = {}) {
	        return new GitSyncRes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.results = this.convertValues(source["results"], GitSyncResult);
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
	
	export class StartAutoSyncReq {
	    repos: GitRepo[];
	
	    static createFrom(source: any = {}) {
	        return new StartAutoSyncReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repos = this.convertValues(source["repos"], GitRepo);
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
	export class StartAutoSyncRes {
	    success: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new StartAutoSyncRes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	    }
	}
	export class WindowState {
	    width: number;
	    height: number;
	    x: number;
	    y: number;
	    maximized: number;
	
	    static createFrom(source: any = {}) {
	        return new WindowState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.width = source["width"];
	        this.height = source["height"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.maximized = source["maximized"];
	    }
	}

}


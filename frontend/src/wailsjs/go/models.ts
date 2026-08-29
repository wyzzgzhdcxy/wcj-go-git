export namespace main {
	
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
	    repo?: types.GitRepo;
	    isGitRepo: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GetGitRepoInfoRes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.repo = this.convertValues(source["repo"], types.GitRepo);
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
	export class PackageReq {
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new PackageReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	    }
	}
	export class PackageResult {
	    success: boolean;
	    message: string;
	    output: string;
	    outputDir: string;
	
	    static createFrom(source: any = {}) {
	        return new PackageResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.output = source["output"];
	        this.outputDir = source["outputDir"];
	    }
	}
	export class ResetReq {
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new ResetReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	    }
	}
	export class ResetResult {
	    success: boolean;
	    message: string;
	    output: string;
	
	    static createFrom(source: any = {}) {
	        return new ResetResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.output = source["output"];
	    }
	}
	export class SoftResetReq {
	    path: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new SoftResetReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.message = source["message"];
	    }
	}
	export class SoftResetResult {
	    success: boolean;
	    message: string;
	    output: string;
	
	    static createFrom(source: any = {}) {
	        return new SoftResetResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.output = source["output"];
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

export namespace types {
	
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
	    commitOnly: boolean;
	    interval: number;
	    lastSyncSuccess: number;
	
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
	        this.commitOnly = source["commitOnly"];
	        this.interval = source["interval"];
	        this.lastSyncSuccess = source["lastSyncSuccess"];
	    }
	}
	export class RepoListReq {
	    repos: GitRepo[];
	
	    static createFrom(source: any = {}) {
	        return new RepoListReq(source);
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
	export class RepoListRes {
	    success: boolean;
	    message: string;
	    repos: GitRepo[];
	
	    static createFrom(source: any = {}) {
	        return new RepoListRes(source);
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
	export class SyncLog {
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
	        return new SyncLog(source);
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
	export class SyncLogsReq {
	    repoPath: string;
	    limit: number;
	
	    static createFrom(source: any = {}) {
	        return new SyncLogsReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repoPath = source["repoPath"];
	        this.limit = source["limit"];
	    }
	}
	export class SyncLogsRes {
	    success: boolean;
	    message: string;
	    logs: SyncLog[];
	
	    static createFrom(source: any = {}) {
	        return new SyncLogsRes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.logs = this.convertValues(source["logs"], SyncLog);
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
	export class SyncReq {
	    repos: GitRepo[];
	
	    static createFrom(source: any = {}) {
	        return new SyncReq(source);
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
	export class SyncResult {
	    path: string;
	    name: string;
	    success: boolean;
	    message: string;
	    pullLog: string;
	    pushLog: string;
	    commitLog: string;
	    committed: boolean;
	    pushed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SyncResult(source);
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
	        this.committed = source["committed"];
	        this.pushed = source["pushed"];
	    }
	}
	export class SyncRes {
	    success: boolean;
	    message: string;
	    results: SyncResult[];
	
	    static createFrom(source: any = {}) {
	        return new SyncRes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.results = this.convertValues(source["results"], SyncResult);
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


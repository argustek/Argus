export namespace git {
	
	export class BranchInfo {
	    name: string;
	    current: boolean;
	    is_remote: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BranchInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.current = source["current"];
	        this.is_remote = source["is_remote"];
	    }
	}
	export class CommitLogEntry {
	    hash: string;
	    short_hash: string;
	    author: string;
	    message: string;
	    date: string;
	
	    static createFrom(source: any = {}) {
	        return new CommitLogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.short_hash = source["short_hash"];
	        this.author = source["author"];
	        this.message = source["message"];
	        this.date = source["date"];
	    }
	}
	export class RemoteInfo {
	    name: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.url = source["url"];
	    }
	}

}

export namespace main {
	
	export class APIConfig {
	    id: string;
	    name: string;
	    provider: string;
	    baseUrl: string;
	    apiKey: string;
	    modelName: string;
	    isDefault: boolean;
	    supportsMultimodal: boolean;
	    testPassed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new APIConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.provider = source["provider"];
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.modelName = source["modelName"];
	        this.isDefault = source["isDefault"];
	        this.supportsMultimodal = source["supportsMultimodal"];
	        this.testPassed = source["testPassed"];
	    }
	}
	export class APITestResult {
	    success: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new APITestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	    }
	}
	export class Change {
	    type: string;
	    file: string;
	
	    static createFrom(source: any = {}) {
	        return new Change(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.file = source["file"];
	    }
	}
	export class ChangeRecord {
	    time: string;
	    title: string;
	    changes: Change[];
	
	    static createFrom(source: any = {}) {
	        return new ChangeRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.title = source["title"];
	        this.changes = this.convertValues(source["changes"], Change);
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
	export class CodeBlock {
	    language: string;
	    code: string;
	    file?: string;
	
	    static createFrom(source: any = {}) {
	        return new CodeBlock(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.language = source["language"];
	        this.code = source["code"];
	        this.file = source["file"];
	    }
	}
	export class ChatMessage {
	    id: number;
	    role: string;
	    content: string;
	    summary?: string;
	    description?: string;
	    changes?: Change[];
	    codeBlocks?: CodeBlock[];
	    error?: string;
	    timestamp?: number;
	
	    static createFrom(source: any = {}) {
	        return new ChatMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.summary = source["summary"];
	        this.description = source["description"];
	        this.changes = this.convertValues(source["changes"], Change);
	        this.codeBlocks = this.convertValues(source["codeBlocks"], CodeBlock);
	        this.error = source["error"];
	        this.timestamp = source["timestamp"];
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
	export class CheckResult {
	    installed: boolean;
	    version?: string;
	    message?: string;
	    can_auto_install?: boolean;
	    install_cmd?: string;
	
	    static createFrom(source: any = {}) {
	        return new CheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.version = source["version"];
	        this.message = source["message"];
	        this.can_auto_install = source["can_auto_install"];
	        this.install_cmd = source["install_cmd"];
	    }
	}
	
	export class HTTPConfig {
	    enabled: boolean;
	    port: number;
	    apiToken: string;
	    allowRemote: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HTTPConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.port = source["port"];
	        this.apiToken = source["apiToken"];
	        this.allowRemote = source["allowRemote"];
	    }
	}
	export class DingTalkConfig {
	    enabled: boolean;
	    name: string;
	    clientId: string;
	    clientSecret: string;
	    robotCode: string;
	    apiUrl: string;
	    mode: string;
	    encrypt: boolean;
	    defaultReply: string;
	    pollInterval: number;
	
	    static createFrom(source: any = {}) {
	        return new DingTalkConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.name = source["name"];
	        this.clientId = source["clientId"];
	        this.clientSecret = source["clientSecret"];
	        this.robotCode = source["robotCode"];
	        this.apiUrl = source["apiUrl"];
	        this.mode = source["mode"];
	        this.encrypt = source["encrypt"];
	        this.defaultReply = source["defaultReply"];
	        this.pollInterval = source["pollInterval"];
	    }
	}
	export class IMConfig {
	    id: string;
	    name: string;
	    provider: string;
	    enabled: boolean;
	    clientId?: string;
	    clientSecret?: string;
	    robotCode?: string;
	    mode?: string;
	    apiUrl?: string;
	    corpId?: string;
	    corpSecret?: string;
	    agentId?: string;
	    appId?: string;
	    appSecret?: string;
	
	    static createFrom(source: any = {}) {
	        return new IMConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.provider = source["provider"];
	        this.enabled = source["enabled"];
	        this.clientId = source["clientId"];
	        this.clientSecret = source["clientSecret"];
	        this.robotCode = source["robotCode"];
	        this.mode = source["mode"];
	        this.apiUrl = source["apiUrl"];
	        this.corpId = source["corpId"];
	        this.corpSecret = source["corpSecret"];
	        this.agentId = source["agentId"];
	        this.appId = source["appId"];
	        this.appSecret = source["appSecret"];
	    }
	}
	export class Config {
	    apiConfigs: APIConfig[];
	    imConfigs: IMConfig[];
	    showCodeBlocks: boolean;
	    showThinking: boolean;
	    pmDecisionAlert: boolean;
	    workDir: string;
	    recentProjects: string[];
	    dingtalk?: DingTalkConfig;
	    http: HTTPConfig;
	    apEnabled: boolean;
	    apConfig?: APIConfig;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiConfigs = this.convertValues(source["apiConfigs"], APIConfig);
	        this.imConfigs = this.convertValues(source["imConfigs"], IMConfig);
	        this.showCodeBlocks = source["showCodeBlocks"];
	        this.showThinking = source["showThinking"];
	        this.pmDecisionAlert = source["pmDecisionAlert"];
	        this.workDir = source["workDir"];
	        this.recentProjects = source["recentProjects"];
	        this.dingtalk = this.convertValues(source["dingtalk"], DingTalkConfig);
	        this.http = this.convertValues(source["http"], HTTPConfig);
	        this.apEnabled = source["apEnabled"];
	        this.apConfig = this.convertValues(source["apConfig"], APIConfig);
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
	
	export class GitStatusEntry {
	    status: string;
	    path: string;
	    name: string;
	    isDir: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GitStatusEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.path = source["path"];
	        this.name = source["name"];
	        this.isDir = source["isDir"];
	    }
	}
	
	
	export class MonitorStatus {
	    isRunning: boolean;
	    pmStatus: string;
	    seStatus: string;
	    cStatus: string;
	    // Go type: time
	    lastCheckTime: any;
	    alertMessage: string;
	    projectState: string;
	
	    static createFrom(source: any = {}) {
	        return new MonitorStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isRunning = source["isRunning"];
	        this.pmStatus = source["pmStatus"];
	        this.seStatus = source["seStatus"];
	        this.cStatus = source["cStatus"];
	        this.lastCheckTime = this.convertValues(source["lastCheckTime"], null);
	        this.alertMessage = source["alertMessage"];
	        this.projectState = source["projectState"];
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
	export class VerificationResult {
	    check: string;
	    passed: boolean;
	    message: string;
	    timestamp: string;
	
	    static createFrom(source: any = {}) {
	        return new VerificationResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.check = source["check"];
	        this.passed = source["passed"];
	        this.message = source["message"];
	        this.timestamp = source["timestamp"];
	    }
	}
	export class PMReviewResult {
	    taskId: string;
	    passed: boolean;
	    checks: VerificationResult[];
	    summary: string;
	    needsFix: string[];
	    timestamp: string;
	
	    static createFrom(source: any = {}) {
	        return new PMReviewResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.taskId = source["taskId"];
	        this.passed = source["passed"];
	        this.checks = this.convertValues(source["checks"], VerificationResult);
	        this.summary = source["summary"];
	        this.needsFix = source["needsFix"];
	        this.timestamp = source["timestamp"];
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
	export class SEWorker {
	    id: string;
	    name: string;
	    status: string;
	    currentTask: string;
	    startedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SEWorker(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.status = source["status"];
	        this.currentTask = source["currentTask"];
	        this.startedAt = source["startedAt"];
	    }
	}
	export class TestResult {
	    passed: boolean;
	    total: number;
	    failed: number;
	    errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new TestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.passed = source["passed"];
	        this.total = source["total"];
	        this.failed = source["failed"];
	        this.errors = source["errors"];
	    }
	}

}

export namespace types {
	
	export class CommandRule {
	    pattern: string;
	    level: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new CommandRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pattern = source["pattern"];
	        this.level = source["level"];
	        this.description = source["description"];
	    }
	}
	export class CommandPolicy {
	    version: number;
	    rules: CommandRule[];
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new CommandPolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.rules = this.convertValues(source["rules"], CommandRule);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	
	export class DecisionRule {
	    type: string;
	    mode: string;
	    default_mode: string;
	    description: string;
	    category: string;
	
	    static createFrom(source: any = {}) {
	        return new DecisionRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.mode = source["mode"];
	        this.default_mode = source["default_mode"];
	        this.description = source["description"];
	        this.category = source["category"];
	    }
	}
	export class DecisionConfig {
	    version: number;
	    rules: DecisionRule[];
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new DecisionConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.rules = this.convertValues(source["rules"], DecisionRule);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	
	export class EnvConfig {
	    value: string;
	    // Go type: time
	    updated_at: any;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new EnvConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.source = source["source"];
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
	export class SystemInfo {
	    os: string;
	    cpu: string;
	    ram: string;
	    go_version: string;
	    node_version: string;
	
	    static createFrom(source: any = {}) {
	        return new SystemInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.os = source["os"];
	        this.cpu = source["cpu"];
	        this.ram = source["ram"];
	        this.go_version = source["go_version"];
	        this.node_version = source["node_version"];
	    }
	}
	export class ToolInfo {
	    path: string;
	    // Go type: time
	    first_seen: any;
	    // Go type: time
	    last_used: any;
	    use_count: number;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.first_seen = this.convertValues(source["first_seen"], null);
	        this.last_used = this.convertValues(source["last_used"], null);
	        this.use_count = source["use_count"];
	        this.source = source["source"];
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
	export class EnvMemory {
	    version: number;
	    tools: Record<string, ToolInfo>;
	    configs: Record<string, EnvConfig>;
	    system: SystemInfo;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new EnvMemory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.tools = this.convertValues(source["tools"], ToolInfo, true);
	        this.configs = this.convertValues(source["configs"], EnvConfig, true);
	        this.system = this.convertValues(source["system"], SystemInfo);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class PathRule {
	    path_pattern: string;
	    permission: string;
	    description: string;
	    is_directory: boolean;
	    priority: number;
	
	    static createFrom(source: any = {}) {
	        return new PathRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path_pattern = source["path_pattern"];
	        this.permission = source["permission"];
	        this.description = source["description"];
	        this.is_directory = source["is_directory"];
	        this.priority = source["priority"];
	    }
	}
	export class PermissionConfig {
	    version: number;
	    rules: PathRule[];
	    default_permission: string;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new PermissionConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.rules = this.convertValues(source["rules"], PathRule);
	        this.default_permission = source["default_permission"];
	        this.updated_at = this.convertValues(source["updated_at"], null);
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


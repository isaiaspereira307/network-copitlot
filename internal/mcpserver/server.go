package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/extensions"
	"github.com/isaiaspereira307/network-copitlot/internal/intruder"
	"github.com/isaiaspereira307/network-copitlot/internal/macro"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
	"github.com/isaiaspereira307/network-copitlot/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"
)

// Server wraps the mcp-go SDK e expoe as 7 tools v2 via stdio.
type Server struct {
	active       *projects.ActiveState
	repo         *projects.Repo
	audit        *audit.Logger
	mcp          *mcpsdk.MCPServer
	currentStore store.Store
	mu           sync.Mutex // guards currentStore
	tools        map[string]toolFunc

	// v3.0 engines (lazy-initialized on first use).
	engine   *intruder.Engine
	macros   *macro.Manager
	engInit  sync.Once
	macInit  sync.Once

	// v4.0 scanner jobs.
	scanMu  sync.Mutex
	scans   map[string]*scanJob

	// v5.0 extensions.
	extMgr  *extensions.Manager
}

func New(active *projects.ActiveState, repo *projects.Repo, a *audit.Logger) *Server {
	m := mcpsdk.NewMCPServer("mcp-proxy", "v0.2.0")
	s := &Server{active: active, repo: repo, audit: a, mcp: m, tools: map[string]toolFunc{}, scans: map[string]*scanJob{}}
	s.RegisterTools()
	return s
}

// RegisterTools registra as 7 tools v2 no server mcp-go. Idempotente.
func (s *Server) RegisterTools() {
	registerV2Tools(s)
	registerV3Tools(s)
	registerV4Tools(s)
	registerV5Tools(s)
	registerV6Tools(s)
	registerV7Tools(s)
	registerV8Tools(s)
	registerV9Tools(s)
	registerV10Tools(s)
	if s.mcp.GetTool("create_project") != nil {
		return // ja registradas
	}
	s.mcp.AddTool(
		mcp.NewTool("create_project",
			mcp.WithDescription("Cria um novo projeto (engajamento) de bug bounty ou pentest"),
			mcp.WithString("name", mcp.Required()),
			mcp.WithString("type", mcp.Required(), mcp.Enum("bugbounty", "pentest")),
			mcp.WithString("program"),
			mcp.WithString("platform"),
		),
		s.wrapTool("create_project", s.toolCreateProject),
	)
	s.mcp.AddTool(
		mcp.NewTool("list_projects",
			mcp.WithDescription("Lista todos os projetos"),
		),
		s.wrapTool("list_projects", s.toolListProjects),
	)
	s.mcp.AddTool(
		mcp.NewTool("set_active_project",
			mcp.WithDescription("Define o projeto ativo"),
			mcp.WithString("name", mcp.Required()),
		),
		s.wrapTool("set_active_project", s.toolSetActiveProject),
	)
	s.mcp.AddTool(
		mcp.NewTool("add_target",
			mcp.WithDescription("Adiciona alvo ao projeto ativo. Cliente MCP deve passar confirmed=true (PRD §5)"),
			mcp.WithString("host", mcp.Required()),
			mcp.WithBoolean("confirmed", mcp.Required()),
			mcp.WithArray("in_scope", mcp.WithStringItems()),
			mcp.WithArray("out_of_scope", mcp.WithStringItems()),
		),
		s.wrapTool("add_target", s.toolAddTarget),
	)
	s.mcp.AddTool(
		mcp.NewTool("list_targets",
			mcp.WithDescription("Lista alvos do projeto ativo"),
		),
		s.wrapTool("list_targets", s.toolListTargets),
	)
	s.mcp.AddTool(
		mcp.NewTool("set_active_target",
			mcp.WithDescription("Define o alvo ativo no projeto ativo"),
			mcp.WithString("host", mcp.Required()),
		),
		s.wrapTool("set_active_target", s.toolSetActiveTarget),
	)
	s.mcp.AddTool(
		mcp.NewTool("get_active_context",
			mcp.WithDescription("Retorna projeto/alvo ativos e contagem de requests"),
		),
		s.wrapTool("get_active_context", s.toolGetActiveContext),
	)
	s.mcp.AddTool(
		mcp.NewTool("list_requests",
			mcp.WithDescription("List captured requests paginated by recency. Returns ONLY summary fields (id, ts, method, url, status, resp_len) — never bodies. Use filters to narrow; default limit=50. Page with offset/since_id. Pick interesting ids then call get_request_detail."),
			mcp.WithString("method_filter"),
			mcp.WithNumber("status_filter"),
			mcp.WithString("host_filter"),
			mcp.WithString("path_contains"),
			mcp.WithNumber("limit"),
			mcp.WithNumber("offset"),
			mcp.WithNumber("since_id"),
		),
		s.wrapTool("list_requests", s.toolListRequests),
	)
	s.mcp.AddTool(
		mcp.NewTool("get_request_detail",
			mcp.WithDescription("Get full detail of one captured request (id from list_requests). include: headers (default, cheapest), body, or all. Bodies are truncated to max_body_bytes (default 8192) — check body_truncated/total_len; use body_range (e.g. '0-4096') to page through large bodies. Never request full bodies of large assets; use search_bodies or summarize first."),
			mcp.WithNumber("id", mcp.Required()),
			mcp.WithString("include", mcp.Enum("headers", "body", "all")),
			mcp.WithNumber("max_body_bytes"),
			mcp.WithString("body_range"),
		),
		s.wrapTool("get_request_detail", s.toolGetRequestDetail),
	)
	s.mcp.AddTool(
		mcp.NewTool("search_bodies",
			mcp.WithDescription("Search request and response bodies of the active target for a pattern: regex if the query compiles as one, otherwise a case-sensitive substring. Returns short ±80-char snippets with request ids and urls — never full bodies. Scope to request bodies (req), response bodies (resp), or both (default). Follow up interesting hits with get_request_detail for the full request."),
			mcp.WithString("query", mcp.Required()),
			mcp.WithString("scope", mcp.Enum("req", "resp", "both")),
			mcp.WithNumber("limit"),
		),
		s.wrapTool("search_bodies", s.toolSearchBodies),
	)
	s.mcp.AddTool(
		mcp.NewTool("replay_request",
			mcp.WithDescription("Re-executes a captured request (id from list_requests) against its target host, guarded by the active target's scope. Optional overrides: url (new target URL), method, headers (string map to set), body (string), follow_redirects (default false — a 3xx is returned as-is; when true, each redirect is re-checked against scope). The replayed exchange is persisted as a NEW request; the result returns only new_request_id, status, resp_len — never bodies. Out-of-scope hosts are blocked with a clear error."),
			mcp.WithNumber("id", mcp.Required()),
			mcp.WithString("url"),
			mcp.WithString("method"),
			mcp.WithObject("headers", mcp.WithStringItems()),
			mcp.WithString("body"),
			mcp.WithBoolean("follow_redirects"),
		),
		s.wrapTool("replay_request", s.toolReplayRequest),
	)
	s.mcp.AddTool(
		mcp.NewTool("set_scope",
			mcp.WithDescription("Persist new in-scope patterns for the ACTIVE target. Writes in_scope to the target's meta.yaml on disk, overwriting previous in-scope patterns; existing out-of-scope patterns are preserved. The live proxy picks up the change automatically on its next request via a cheap mtime check of the meta.yaml — no proxy restart required. Pass an empty array to clear all in-scope restrictions (anything not explicitly out-of-scope becomes allowed)."),
			mcp.WithArray("in_scope", mcp.Required(), mcp.WithStringItems()),
		),
		s.wrapTool("set_scope", s.toolSetScope),
	)
	s.mcp.AddTool(
		mcp.NewTool("list_endpoints",
			mcp.WithDescription("List deduplicated API endpoints for the active target. Dynamic path segments (numeric ids, UUIDs, long hex or base64 tokens) are normalized to {id}, so /users/123 and /users/456 collapse into one endpoint. Each entry reports the HTTP method, normalized path, total hit count, and up to 5 sample request ids for follow-up with get_request_detail. Use this to map the target's API surface before diving into individual requests."),
		),
		s.wrapTool("list_endpoints", s.toolListEndpoints),
	)
	s.mcp.AddTool(
		mcp.NewTool("diff_requests",
			mcp.WithDescription("Compare two captured requests of the active target with a minimal line-based unified diff (prefix ' ' unchanged, '-' only in A, '+' only in B). mode selects what to diff: resp (default, response bodies), req (request bodies), or headers (request+response headers as lines). Returns changed_a/changed_b counts plus a truncated diff (truncated=true, total=... when over budget) so large bodies never flood the context window. Pass ids from list_requests to find what changed between two exchanges, e.g. a token rotating or an API shape change."),
			mcp.WithNumber("id_a", mcp.Required()),
			mcp.WithNumber("id_b", mcp.Required()),
			mcp.WithString("mode", mcp.Enum("resp", "req", "headers")),
		),
		s.wrapTool("diff_requests", s.toolDiffRequests),
	)
	s.mcp.AddTool(
		mcp.NewTool("summarize_response",
			mcp.WithDescription("Summarize the response body of one captured request (id from list_requests) by content type. HTML: forms (action/method/fields), links, external/inline scripts, and interesting comments; JSON: a key/type map — values are never included; JS: endpoint URLs, fetch/XHR/axios call targets, and token hints (JWT, AWS, API keys — truncated to 8 chars). Returns a compact structured summary, never the raw body; oversized bodies are analyzed from a capped prefix (truncated=true, total_len=N). Use to triage responses cheaply before opening one with get_request_detail."),
			mcp.WithNumber("id", mcp.Required()),
		),
		s.wrapTool("summarize_response", s.toolSummarizeResponse),
	)
	s.mcp.AddTool(
		mcp.NewTool("fuzz_request",
			mcp.WithDescription("Intruder-style fuzzing: take one captured request (id from list_requests) and replay it once per payload, injecting each payload at a chosen point, always under the active target's scope guard. point selects where to inject: 'marker' (replace every occurrence of `marker`, default FUZZ, across url+body+header values), 'body' (whole body), 'url' (whole url), 'query:<param>' (set that query param), or 'header:<name>' (set that header). Supply values via payloads (string array) and/or payload_set (builtin: xss, sqli, traversal, redirect); capped at 100 payloads. Each replay is persisted as a new request; the tool returns a compact table — payload, status, resp_len, reflected (payload echoed in the response body), new_id, anomaly — never full bodies. Rows flagged anomaly (status changed vs baseline, size changed >20%, or reflected) sort first; the list is truncated for token frugality (truncated=true). Follow anomalies with get_request_detail on new_id. Out-of-scope hosts are blocked per payload."),
			mcp.WithNumber("id", mcp.Required()),
			mcp.WithString("point", mcp.Required()),
			mcp.WithArray("payloads", mcp.WithStringItems()),
			mcp.WithString("payload_set", mcp.Enum("xss", "sqli", "traversal", "redirect")),
			mcp.WithString("marker"),
			mcp.WithBoolean("follow_redirects"),
		),
		s.wrapTool("fuzz_request", s.toolFuzzRequest),
	)
	s.mcp.AddTool(
		mcp.NewTool("set_match_replace",
			mcp.WithDescription("Persist live match/replace rules for the ACTIVE target (replaces the whole rule list; pass an empty array to clear). The running proxy applies enabled rules to every in-scope request before forwarding, picking up changes live via an mtime check of meta.yaml — no restart. Each rule: part (url | req_header | req_body), match (RE2 regex), replace (supports $1/${name}), header (required when part=req_header), name (optional label), enabled (default true). Rules are validated (part valid, regex compiles) before saving. A url rule whose rewrite would move the host out of scope is skipped at runtime — match/replace never leaks traffic off the authorized target."),
			mcp.WithArray("rules", mcp.Required()),
		),
		s.wrapTool("set_match_replace", s.toolSetMatchReplace),
	)
	s.mcp.AddTool(
		mcp.NewTool("list_match_replace",
			mcp.WithDescription("List the persisted live match/replace rules of the active target (reloaded from disk)."),
		),
		s.wrapTool("list_match_replace", s.toolListMatchReplace),
	)
	s.mcp.AddTool(
		mcp.NewTool("intruder_start",
			mcp.WithDescription("Launch an asynchronous Intruder fuzzing job (v3). Replays a captured base request (base_request_id from list_requests) once per generated case. attack_type: sniper (each payload set over one position at a time), battering_ram (same payload to all positions per row), pitchfork (parallel sets, row i uses payload i of each), cluster_bomb (Cartesian product). positions: array selecting injection points, e.g. 'url', 'body', 'query:<param>', 'header:<name>'. Payloads come from payload_sets (array of arrays, one per position), payload_set (builtin xss|sqli|traversal|redirect applied to every position), or payloads (single array applied to every position). throttling via throttle_rps (0=unlimited). Every replay is scope-guarded and persisted as a new request. Returns a job_id; poll with intruder_status/intruder_results. Cap: 2000 cases per job."),
			mcp.WithNumber("base_request_id", mcp.Required()),
			mcp.WithString("attack_type", mcp.Required(), mcp.Enum("sniper", "battering_ram", "pitchfork", "cluster_bomb")),
			mcp.WithArray("positions", mcp.Required(), mcp.WithStringItems()),
			mcp.WithArray("payload_sets"),
			mcp.WithString("payload_set", mcp.Enum("xss", "sqli", "traversal", "redirect")),
			mcp.WithArray("payloads", mcp.WithStringItems()),
			mcp.WithNumber("throttle_rps"),
		),
		s.wrapTool("intruder_start", s.toolIntruderStart),
	)
	s.mcp.AddTool(
		mcp.NewTool("intruder_status",
			mcp.WithDescription("Check the status/progress of an intruder job by job_id from intruder_start. Returns status (queued|running|done|cancelled|error), done/total_cases, anomalies count."),
			mcp.WithString("job_id", mcp.Required()),
		),
		s.wrapTool("intruder_status", s.toolIntruderStatus),
	)
	s.mcp.AddTool(
		mcp.NewTool("intruder_results",
			mcp.WithDescription("Fetch the results of a completed intruder job (job_id from intruder_start). Returns aggregated by_status counts plus the full anomalous cases list (status, resp_len, replay_id). Optional 'grep' returns only rows whose new response body contains the substring. Results are token-frugal — no full bodies."),
			mcp.WithString("job_id", mcp.Required()),
			mcp.WithString("grep"),
		),
		s.wrapTool("intruder_results", s.toolIntruderResults),
	)
	s.mcp.AddTool(
		mcp.NewTool("intruder_cancel",
			mcp.WithDescription("Cancel a running intruder job by job_id from intruder_start."),
			mcp.WithString("job_id", mcp.Required()),
		),
		s.wrapTool("intruder_cancel", s.toolIntruderCancel),
	)
	s.mcp.AddTool(
		mcp.NewTool("macro_record",
			mcp.WithDescription("Save a macro (session-handling chain, v3) under a name for later macro_play. A macro is an ordered list of steps; each step: method, url, headers (optional map), body (optional), extractors (optional array of {name, pattern} regex with 1 capture group to pull session variables from the step's response). Variables are referenced in later steps as {name}. Steps are executed under the active target's scope guard."),
			mcp.WithString("name", mcp.Required()),
			mcp.WithArray("steps", mcp.Required()),
		),
		s.wrapTool("macro_record", s.toolMacroRecord),
	)
	s.mcp.AddTool(
		mcp.NewTool("macro_play",
			mcp.WithDescription("Execute a saved macro by name (from macro_record) to establish/maintain a session. Runs each step against the active target under scope guard, extracting {var} from responses via the step's extractors and substituting into later steps. Returns the session_id, steps_run, last status, and extracted vars. Pass an existing session_id to continue reusing its variables."),
			mcp.WithString("name", mcp.Required()),
			mcp.WithString("session_id"),
		),
		s.wrapTool("macro_play", s.toolMacroPlay),
	)
	s.mcp.AddTool(
		mcp.NewTool("macro_list",
			mcp.WithDescription("List the names of all saved macros."),
		),
		s.wrapTool("macro_list", s.toolMacroList),
	)
	s.mcp.AddTool(
		mcp.NewTool("scan_passive_run",
			mcp.WithDescription("Run the passive scanner (v4) over all ALREADY captured requests of the active target — sends NOTHING to the host, it only reads stored traffic. Detectors: reflected XSS, SQLi error patterns, SSRF hints, open redirect, secrets in JS, IDOR hints. Results are persisted as findings (list_findings). Returns a job_id; poll with scan_passive_status."),
		),
		s.wrapTool("scan_passive_run", s.toolScanPassiveRun),
	)
	s.mcp.AddTool(
		mcp.NewTool("scan_passive_status",
			mcp.WithDescription("Check the progress of a passive scan job by job_id from scan_passive_run. Returns status, requests scanned, and hit counts per finding type."),
			mcp.WithString("job_id", mcp.Required()),
		),
		s.wrapTool("scan_passive_status", s.toolScanPassiveStatus),
	)
	s.mcp.AddTool(
		mcp.NewTool("list_findings",
			mcp.WithDescription("List findings of the active target, optionally filtered by type (XSS|IDOR|SQLi|SSRF|redirect|secret|other) and severity (info|low|med|high|crit). Ordered by severity then recency. Follow up with get_finding_detail."),
			mcp.WithString("type"),
			mcp.WithString("severity", mcp.Enum("info", "low", "med", "high", "crit")),
		),
		s.wrapTool("list_findings", s.toolListFindings),
	)
	s.mcp.AddTool(
		mcp.NewTool("get_finding_detail",
			mcp.WithDescription("Get the full detail of one finding (finding_id from list_findings): type, severity, url, request_id, status, notes, and structured evidence."),
			mcp.WithNumber("finding_id", mcp.Required()),
		),
		s.wrapTool("get_finding_detail", s.toolGetFindingDetail),
	)
	s.mcp.AddTool(
		mcp.NewTool("finding_set_status",
			mcp.WithDescription("Update the lifecycle status of a finding (finding_id from list_findings): open|triaged|confirmed|false-positive|closed."),
			mcp.WithNumber("finding_id", mcp.Required()),
			mcp.WithString("status", mcp.Required(), mcp.Enum("open", "triaged", "confirmed", "false-positive", "closed")),
		),
		s.wrapTool("finding_set_status", s.toolFindingSetStatus),
	)
	s.mcp.AddTool(
		mcp.NewTool("get_sitemap",
			mcp.WithDescription("Get the passive sitemap of the active target: deduplicated endpoint tree (method + path, dynamic segments collapsed to {id}) with hit counts, derived from captured traffic."),
		),
		s.wrapTool("get_sitemap", s.toolGetSitemap),
	)
	s.mcp.AddTool(
		mcp.NewTool("scan_active_start",
			mcp.WithDescription("Run the ACTIVE scanner (v4.1): sends safe non-destructive payloads (XSS/SQLi/SSRF/redirect test strings) to the active target in-scope with aggressive throttle, to detect reflection. REQUIRES double opt-in: server started with MCP_PROXY_ACTIVE=1 AND confirmed=true. Reflected payloads become findings."),
			mcp.WithBoolean("confirmed", mcp.Required()),
			mcp.WithNumber("throttle_rps"),
		),
		s.wrapTool("scan_active_start", s.toolScanActiveStart),
	)
	s.mcp.AddTool(
		mcp.NewTool("scan_active_status",
			mcp.WithDescription("Check the progress of an active scan job (job_id from scan_active_start)."),
			mcp.WithString("job_id", mcp.Required()),
		),
		s.wrapTool("scan_active_status", s.toolScanActiveStatus),
	)
	s.mcp.AddTool(
		mcp.NewTool("crawl_start",
			mcp.WithDescription("Start an active crawler against the active target to discover same-host URLs. Requires MCP_PROXY_ACTIVE=1 and confirmed=true. (Crawler harness implementation pending; use scan_passive_run + get_sitemap meanwhile.)"),
			mcp.WithBoolean("confirmed", mcp.Required()),
		),
		s.wrapTool("crawl_start", s.toolCrawlStart),
	)
	s.mcp.AddTool(
		mcp.NewTool("decode",
			mcp.WithDescription("Decode a value in a format: base64, url, hex, html, jwt (payload), gzip."),
			mcp.WithString("format", mcp.Required(), mcp.Enum("base64", "url", "hex", "html", "jwt", "gzip")),
			mcp.WithString("input", mcp.Required()),
		),
		s.wrapTool("decode", s.toolDecode),
	)
	s.mcp.AddTool(
		mcp.NewTool("encode",
			mcp.WithDescription("Encode a value in a format: base64, url, hex, html, jwt (payload), gzip."),
			mcp.WithString("format", mcp.Required(), mcp.Enum("base64", "url", "hex", "html", "jwt", "gzip")),
			mcp.WithString("input", mcp.Required()),
		),
		s.wrapTool("encode", s.toolEncode),
	)
	s.mcp.AddTool(
		mcp.NewTool("compare",
			mcp.WithDescription("Visual diff of two captured requests/responses. kind: request|response|headers."),
			mcp.WithNumber("left_id", mcp.Required()),
			mcp.WithNumber("right_id", mcp.Required()),
			mcp.WithString("kind", mcp.Enum("request", "response", "headers")),
		),
		s.wrapTool("compare", s.toolCompare),
	)
	s.mcp.AddTool(
		mcp.NewTool("tag_request",
			mcp.WithDescription("Attach a custom tag to a captured request (Logger++)."),
			mcp.WithNumber("request_id", mcp.Required()),
			mcp.WithString("tag", mcp.Required()),
		),
		s.wrapTool("tag_request", s.toolTagRequest),
	)
	s.mcp.AddTool(
		mcp.NewTool("add_comment",
			mcp.WithDescription("Attach a timestamped comment to a captured request (Logger++)."),
			mcp.WithNumber("request_id", mcp.Required()),
			mcp.WithString("comment", mcp.Required()),
		),
		s.wrapTool("add_comment", s.toolAddComment),
	)
	s.mcp.AddTool(
		mcp.NewTool("list_tags",
			mcp.WithDescription("List all tags in use on the active target (Logger++)."),
		),
		s.wrapTool("list_tags", s.toolListTags),
	)
	s.mcp.AddTool(
		mcp.NewTool("ext_list",
			mcp.WithDescription("List known extensions and their enabled status in the active project (Extensions API v5)."),
		),
		s.wrapTool("ext_list", s.toolExtList),
	)
	s.mcp.AddTool(
		mcp.NewTool("ext_enable",
			mcp.WithDescription("Enable an extension in the active project (allowlist)."),
			mcp.WithString("ext_name", mcp.Required()),
		),
		s.wrapTool("ext_enable", s.toolExtEnable),
	)
	s.mcp.AddTool(
		mcp.NewTool("ext_disable",
			mcp.WithDescription("Disable an extension in the active project (allowlist)."),
			mcp.WithString("ext_name", mcp.Required()),
		),
		s.wrapTool("ext_disable", s.toolExtDisable),
	)
	s.mcp.AddTool(
		mcp.NewTool("report_export_markdown",
			mcp.WithDescription("Export findings of the active target as a HackerOne-ready Markdown report. Returns the file path."),
			mcp.WithString("status_filter", mcp.Enum("open", "triaged", "confirmed", "false-positive", "closed")),
		),
		s.wrapTool("report_export_markdown", s.toolReportMarkdown),
	)
	s.mcp.AddTool(
		mcp.NewTool("report_export_html",
			mcp.WithDescription("Export findings of the active target as an HTML report. Returns the file path."),
			mcp.WithString("status_filter", mcp.Enum("open", "triaged", "confirmed", "false-positive", "closed")),
		),
		s.wrapTool("report_export_html", s.toolReportHTML),
	)
	s.mcp.AddTool(
		mcp.NewTool("report_export_pdf",
			mcp.WithDescription("Export findings of the active target as a PDF report (requires chrome headless; falls back to HTML with guidance)."),
			mcp.WithString("status_filter", mcp.Enum("open", "triaged", "confirmed", "false-positive", "closed")),
		),
		s.wrapTool("report_export_pdf", s.toolReportPDF),
	)
	s.mcp.AddTool(
		mcp.NewTool("export_curl",
			mcp.WithDescription("Rebuild a captured request as a ready-to-paste curl command. Pass a request id from list_requests. Returns the exact one-liner (or heredoc form for large bodies) with method, URL, headers and body."),
			mcp.WithNumber("id", mcp.Required()),
		),
		s.wrapTool("export_curl", s.toolExportCurl),
	)
	s.mcp.AddTool(
		mcp.NewTool("export_har",
			mcp.WithDescription("Export the entire active target as HAR 1.2 — metadata only (method, URL, headers, statuses, sizes), no bodies. Import into other tools (Burp, devtools). Returns the file path."),
		),
		s.wrapTool("export_har", s.toolExportHAR),
	)
	s.mcp.AddTool(
		mcp.NewTool("jwt_decode",
			mcp.WithDescription("Decode a JWT (JWS compact serialization). Returns header, payload, signature and attack-surface warnings: alg=none, empty signature, expired exp. Does not verify the signature."),
			mcp.WithString("token", mcp.Required()),
		),
		s.wrapTool("jwt_decode", s.toolJwtDecode),
	)
}

// wrapTool adapta toolFunc (ctx, map[string]any) para ToolHandlerFunc do mcp-go.
func (s *Server) wrapTool(name string, fn toolFunc) mcpsdk.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := req.Params.Arguments.(map[string]any)
		out, err := fn(ctx, args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

// openStoreForActiveTarget abre (ou recria) o store SQLite do alvo ativo.
// Retorna nil se nao ha alvo ativo. Fecha o anterior se existir.
func (s *Server) openStoreForActiveTarget() (store.Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentStore != nil {
		_ = s.currentStore.Close()
		s.currentStore = nil
	}
	proj, err := s.active.Project()
	if err != nil || proj == nil {
		return nil, nil
	}
	tgt, err := s.active.Target()
	if err != nil || tgt == nil {
		return nil, nil
	}
	targetDir := tgt.Dir(proj.Dir(s.repo.WorkspacePath()))
	dbPath := filepath.Join(targetDir, "requests.db")
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		return nil, err
	}
	s.currentStore = st
	return st, nil
}

// Run inicia o servidor MCP em stdio. Bloqueia ate EOF no stdin.
// ponytail: ctx nao honrado — ServeStdio nao aceita ctx. Wire com goroutine
// + cancelacao manual quando o SDK expor. Manter param evita quebra de API.
func (s *Server) Run(ctx context.Context) error {
	_ = ctx
	return mcpsdk.ServeStdio(s.mcp)
}

// CallTool invoca uma tool registrada fora do protocolo stdio: harness e2e e
// clientes programaticos. Mesma via que o SDK usa internamente (s.tools).
func (s *Server) CallTool(name string, args map[string]any) (string, error) {
	fn, ok := s.tools[name]
	if !ok {
		return "", fmt.Errorf("tool %s nao registrada", name)
	}
	return fn(context.Background(), args)
}

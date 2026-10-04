// Vault telemetry plugin for the Cline SDK.
//
// Uses the afterTool lifecycle hook to intercept terminal commands the agent
// runs (run_commands in the Cline SDK/CLI, execute_command in the VS Code
// extension) and file edits (write_to_file, replace_file_content, edit_file,
// insert_content) and appends one JSON line per call to .vault/activity.jsonl
// in the workspace root, in the exact shape Vault's Go server expects:
//
//	{"time":...,"kind":"COMMAND"|"EDIT","command":...,"exit_code":...,"stderr":...,"files":[...],"workspace":...}
//
// Observational only: the hook never throws and never modifies tool results.
import type { AgentPlugin } from "@cline/sdk";
import * as fs from "node:fs";
import * as path from "node:path";

// Tool names that execute shell commands across Cline hosts: the SDK/CLI uses
// `run_commands`; the VS Code extension uses `execute_command`. `run_command`
// covers older CLI builds.
const COMMAND_TOOL_NAMES = new Set(["run_commands", "execute_command", "run_command"]);

// Tool names that edit files. Intercepting these keeps H2 (churn) fed with
// snapshots even when the agent edits without running any commands.
const EDIT_TOOL_NAMES = new Set(["write_to_file", "replace_file_content", "edit_file", "insert_content"]);

// Workspace root resolved at setup() time from the host workspace context;
// the hook falls back to VAULT_ROOT or process.cwd() if setup never ran.
let workspaceRoot = "";
let activityPath = "";

interface ActivityRecord {
	kind: "COMMAND" | "EDIT";
	command: string;
	exitCode: number;
	output: string;
	files: string[];
}

/** Extract the command string(s) from a tool input. */
export function extractCommand(input: unknown): string {
	if (typeof input === "string") {
		return input;
	}
	if (input && typeof input === "object") {
		const obj = input as Record<string, unknown>;
		if (Array.isArray(obj.commands)) {
			return obj.commands.filter((c): c is string => typeof c === "string").join("\n");
		}
		if (typeof obj.command === "string") {
			return obj.command;
		}
	}
	return "";
}

/** Extract the edited file path from an edit-tool input. */
export function extractFilePath(input: unknown): string {
	if (input && typeof input === "object") {
		const obj = input as Record<string, unknown>;
		if (typeof obj.file_path === "string" && obj.file_path.length > 0) {
			return obj.file_path;
		}
		if (typeof obj.path === "string" && obj.path.length > 0) {
			return obj.path;
		}
	}
	return "";
}

/** Parse `Command exited with code N` out of arbitrary text. */
function exitCodeFromText(text: string): number | null {
	const m = text.match(/Command exited with code (\d+)/);
	return m ? Number.parseInt(m[1], 10) : null;
}

interface ExtractedResult {
	output: string;
	exitCode: number;
}

/**
 * Pull combined output and exit code out of a tool result, tolerating the
 * shapes produced by different hosts:
 *   - SDK/CLI run_commands: array of { query, result, error?, success }
 *   - extension execute_command: plain string output
 *   - object form: { stdout, stderr, exitCode }
 */
export function extractResult(result: unknown): ExtractedResult {
	const res = (result ?? {}) as { output?: unknown; isError?: boolean };
	const out = res.output;

	if (Array.isArray(out)) {
		let text = "";
		let code: number | null = null;
		let success = true;
		for (const item of out) {
			const it = (item ?? {}) as { result?: unknown; error?: string; success?: boolean };
			if (typeof it.result === "string" && it.result.length > 0) {
				text += text ? "\n" + it.result : it.result;
			}
			if (it.success === false) {
				success = false;
			}
			if (code === null && typeof it.error === "string") {
				code = exitCodeFromText(it.error);
			}
		}
		if (code === null) {
			code = exitCodeFromText(text);
		}
		return { output: text, exitCode: code ?? (success ? 0 : 1) };
	}

	if (typeof out === "string") {
		const code = exitCodeFromText(out);
		return { output: out, exitCode: code ?? (res.isError ? 1 : 0) };
	}

	if (out && typeof out === "object") {
		const o = out as { stdout?: unknown; stderr?: unknown; exitCode?: unknown; success?: boolean };
		const text = [o.stdout, o.stderr]
			.filter((chunk): chunk is string => typeof chunk === "string" && chunk.length > 0)
			.join("\n");
		const code =
			typeof o.exitCode === "number" ? o.exitCode : (exitCodeFromText(text) ?? (o.success === false ? 1 : 0));
		return { output: text, exitCode: code };
	}

	return { output: "", exitCode: res.isError ? 1 : 0 };
}

/** Keep only the last n lines of text. */
export function lastLines(text: string, n: number): string {
	const lines = text.split("\n");
	if (lines.length <= n) {
		return text;
	}
	return lines.slice(lines.length - n).join("\n");
}

/** Build the single JSON line Vault's activity log expects. */
export function buildLine(rec: ActivityRecord, now: Date, workspace: string): string {
	return (
		JSON.stringify({
			time: now.toISOString(),
			kind: rec.kind,
			command: rec.command,
			exit_code: rec.exitCode,
			stderr: lastLines(rec.output, 40),
			files: rec.files,
			workspace: workspace,
		}) + "\n"
	);
}

/** Resolve the workspace root from the plugin setup context. */
function resolveRoot(setupCtx: unknown): string {
	const ctx = (setupCtx ?? {}) as { workspaceInfo?: { rootPath?: string } };
	if (ctx.workspaceInfo?.rootPath) {
		return ctx.workspaceInfo.rootPath;
	}
	if (process.env.VAULT_ROOT) {
		return process.env.VAULT_ROOT;
	}
	return process.cwd();
}

function appendLine(rec: ActivityRecord): void {
	if (!activityPath) {
		workspaceRoot = resolveRoot(undefined);
		activityPath = path.join(workspaceRoot, ".vault", "activity.jsonl");
	}
	fs.mkdirSync(path.dirname(activityPath), { recursive: true });
	fs.appendFileSync(activityPath, buildLine(rec, new Date(), workspaceRoot), "utf8");
}

const plugin: AgentPlugin = {
	name: "vault-telemetry",
	manifest: { capabilities: ["hooks"] },
	setup(_api, ctx) {
		workspaceRoot = resolveRoot(ctx);
		activityPath = path.join(workspaceRoot, ".vault", "activity.jsonl");
	},
	hooks: {
		afterTool(context) {
			try {
				const toolName = context.toolCall?.toolName ?? context.tool?.name ?? "";
				if (COMMAND_TOOL_NAMES.has(toolName)) {
					const command = extractCommand(context.input);
					if (!command) {
						return;
					}
					const { output, exitCode } = extractResult(context.result);
					appendLine({ kind: "COMMAND", command, exitCode, output, files: [] });
					return;
				}
				if (EDIT_TOOL_NAMES.has(toolName)) {
					const filePath = extractFilePath(context.input);
					if (!filePath) {
						return;
					}
					appendLine({
						kind: "EDIT",
						command: "edited " + filePath,
						exitCode: 0,
						output: "",
						files: [filePath],
					});
				}
			} catch (err) {
				// Observational hook: log and swallow, never fail the tool.
				console.error("[vault-telemetry] afterTool failed:", err);
			}
			return undefined;
		},
	},
};

export { plugin };
export default plugin;

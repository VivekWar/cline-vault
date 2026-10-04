// Vault telemetry plugin for the Cline SDK.
//
// Uses the afterTool lifecycle hook to intercept terminal commands the agent
// runs (run_commands in the Cline SDK/CLI, execute_command in the VS Code
// extension) and file edits (write_to_file, replace_file_content, edit_file,
// insert_content). For each intercepted call it spawns the Vault Go binary's
// `report` subcommand, so the Go server itself takes the git snapshot and
// appends the line to .vault/activity.jsonl — the plugin never writes the
// activity log directly.
//
// Observational only: the hook never throws and never modifies tool results.
import type { AgentPlugin } from "@cline/sdk";
import { spawnSync } from "node:child_process";
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

/**
 * Build the report_activity arguments payload matching Vault's
 * reportActivitySchema (internal/mcp/tools.go). The Go server adds the
 * timestamp itself, so no `time` field is included here.
 */
export function buildPayload(rec: ActivityRecord, workspace: string): string {
	return JSON.stringify({
		kind: rec.kind,
		command: rec.command,
		exit_code: rec.exitCode,
		stderr: lastLines(rec.output, 40),
		files: rec.files,
		workspace: workspace,
	});
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

/** Locate the vault binary: VAULT_BIN, else <workspace>/bin/vault, else PATH. */
function vaultBinary(): string {
	if (process.env.VAULT_BIN) {
		return process.env.VAULT_BIN;
	}
	if (workspaceRoot) {
		const candidate = path.join(workspaceRoot, "bin", "vault");
		if (fs.existsSync(candidate)) {
			return candidate;
		}
	}
	return "vault";
}

/**
 * Report one activity record through the vault binary's `report` subcommand.
 * Going through the Go server is what keeps the `tree` field populated (the
 * server takes the git snapshot), which H2 churn depends on.
 */
function report(rec: ActivityRecord): void {
	if (!workspaceRoot) {
		workspaceRoot = resolveRoot(undefined);
	}
	const payload = buildPayload(rec, workspaceRoot);
	const res = spawnSync(vaultBinary(), ["report", "--root", workspaceRoot, payload], {
		cwd: workspaceRoot,
		timeout: 30000,
		encoding: "utf8",
	});
	if (res.error) {
		console.error("[vault-telemetry] cannot spawn vault binary:", res.error.message);
		return;
	}
	if (res.status !== 0) {
		console.error(
			"[vault-telemetry] vault report failed with status",
			res.status,
			":",
			(res.stderr ?? "").toString().trim(),
		);
	}
}

const plugin: AgentPlugin = {
	name: "vault-telemetry",
	manifest: { capabilities: ["hooks"] },
	setup(_api, ctx) {
		workspaceRoot = resolveRoot(ctx);
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
					report({ kind: "COMMAND", command, exitCode, output, files: [] });
					return;
				}
				if (EDIT_TOOL_NAMES.has(toolName)) {
					const filePath = extractFilePath(context.input);
					if (!filePath) {
						return;
					}
					report({
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

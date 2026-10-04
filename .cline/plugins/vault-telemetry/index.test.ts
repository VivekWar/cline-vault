import { test } from "node:test";
import assert from "node:assert/strict";
import { extractCommand, extractFilePath, extractResult, lastLines, buildLine } from "./index.js";

test("extractCommand: run_commands array input is joined", () => {
	assert.equal(
		extractCommand({ commands: ["go test ./...", "make verify"] }),
		"go test ./...\nmake verify",
	);
});

test("extractCommand: execute_command single command", () => {
	assert.equal(extractCommand({ command: "go build ./cmd/vault" }), "go build ./cmd/vault");
});

test("extractCommand: plain string input", () => {
	assert.equal(extractCommand("ls -la"), "ls -la");
});

test("extractCommand: unknown input yields empty", () => {
	assert.equal(extractCommand(null), "");
	assert.equal(extractCommand({}), "");
	assert.equal(extractCommand(42), "");
});

test("extractFilePath: file_path field", () => {
	assert.equal(extractFilePath({ file_path: "/home/vivek/proj/a.go" }), "/home/vivek/proj/a.go");
});

test("extractFilePath: path field", () => {
	assert.equal(extractFilePath({ path: "src/lib.ts", content: "x" }), "src/lib.ts");
});

test("extractFilePath: file_path wins over path", () => {
	assert.equal(
		extractFilePath({ file_path: "/a.go", path: "/b.go" }),
		"/a.go",
	);
});

test("extractFilePath: missing or non-string yields empty", () => {
	assert.equal(extractFilePath({}), "");
	assert.equal(extractFilePath({ file_path: 7 }), "");
	assert.equal(extractFilePath(null), "");
	assert.equal(extractFilePath("plain string"), "");
});

test("extractResult: SDK success array", () => {
	const r = extractResult({
		output: [{ query: "go test", result: "ok\n", success: true }],
	});
	assert.equal(r.exitCode, 0);
	assert.equal(r.output, "ok\n");
});

test("extractResult: SDK failure array carries exit code from error", () => {
	const r = extractResult({
		output: [
			{
				query: "go test",
				result: "[Command exited with code 1]\n--- FAIL: TestAdd",
				error: "Command exited with code 1",
				success: false,
			},
		],
	});
	assert.equal(r.exitCode, 1);
	assert.ok(r.output.includes("--- FAIL: TestAdd"));
});

test("extractResult: exit code parsed from output text", () => {
	const r = extractResult({ output: "some output\n[Command exited with code 2]\n" });
	assert.equal(r.exitCode, 2);
});

test("extractResult: isError without exit info falls back to 1", () => {
	const r = extractResult({ output: "boom", isError: true });
	assert.equal(r.exitCode, 1);
});

test("extractResult: object shape with stdout/stderr/exitCode", () => {
	const r = extractResult({ output: { stdout: "out", stderr: "err", exitCode: 3 } });
	assert.equal(r.exitCode, 3);
	assert.equal(r.output, "out\nerr");
});

test("extractResult: missing result yields empty output and exit 0", () => {
	const r = extractResult({});
	assert.equal(r.output, "");
	assert.equal(r.exitCode, 0);
});

test("lastLines: keeps only the tail", () => {
	const text = Array.from({ length: 50 }, (_, i) => `line ${i}`).join("\n");
	const out = lastLines(text, 40);
	assert.equal(out.split("\n").length, 40);
	assert.ok(out.startsWith("line 10"));
	assert.ok(out.endsWith("line 49"));
});

test("lastLines: short text unchanged", () => {
	assert.equal(lastLines("a\nb", 40), "a\nb");
});

test("buildLine: exact Vault JSON line shape", () => {
	const line = buildLine(
		{ kind: "COMMAND", command: "go test ./...", exitCode: 1, output: "line1\nline2", files: [] },
		new Date("2026-10-04T12:00:00.000Z"),
		"/home/vivek/proj",
	);
	const parsed = JSON.parse(line);
	assert.equal(parsed.time, "2026-10-04T12:00:00.000Z");
	assert.equal(parsed.kind, "COMMAND");
	assert.equal(parsed.command, "go test ./...");
	assert.equal(parsed.exit_code, 1);
	assert.equal(parsed.stderr, "line1\nline2");
	assert.deepEqual(parsed.files, []);
	assert.equal(parsed.workspace, "/home/vivek/proj");
	assert.ok(line.endsWith("\n"));
});

test("buildLine: stderr truncated to the last 40 lines", () => {
	const big = Array.from({ length: 100 }, (_, i) => `L${i}`).join("\n");
	const line = buildLine(
		{ kind: "COMMAND", command: "x", exitCode: 0, output: big, files: [] },
		new Date("2026-10-04T12:00:00.000Z"),
		"/ws",
	);
	const parsed = JSON.parse(line);
	assert.equal(parsed.stderr.split("\n").length, 40);
	assert.ok(parsed.stderr.startsWith("L60"));
	assert.ok(parsed.stderr.endsWith("L99"));
});

test("buildLine: EDIT kind line shape", () => {
	const line = buildLine(
		{ kind: "EDIT", command: "edited /home/vivek/proj/pkg/calc.go", exitCode: 0, output: "", files: ["/home/vivek/proj/pkg/calc.go"] },
		new Date("2026-10-04T12:00:00.000Z"),
		"/home/vivek/proj",
	);
	const parsed = JSON.parse(line);
	assert.equal(parsed.time, "2026-10-04T12:00:00.000Z");
	assert.equal(parsed.kind, "EDIT");
	assert.equal(parsed.command, "edited /home/vivek/proj/pkg/calc.go");
	assert.equal(parsed.exit_code, 0);
	assert.equal(parsed.stderr, "");
	assert.deepEqual(parsed.files, ["/home/vivek/proj/pkg/calc.go"]);
	assert.equal(parsed.workspace, "/home/vivek/proj");
	assert.ok(line.endsWith("\n"));
});

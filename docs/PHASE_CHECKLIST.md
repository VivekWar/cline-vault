# Vault — End-of-Phase Checklist

Run this at the end of every phase. Substitute N with the phase id (e.g. 1b, 2).

1. Run `make verify` and show the full output. Do not continue unless it is green.

2. Write `docs/phases/PHASE-N.md`, replacing any existing file. Use only facts
   verified by commands you ran, and write "unknown" for anything else. Sections:
   Summary; Acceptance criteria table (Criterion | PASS/FAIL/PARTIAL | Evidence);
   Files built with purpose and line count (wc -l); Design decisions with
   rationale and rejected alternatives; Deviations from the prompt (including
   whether tests were really written first); Problems encountered (root cause,
   fix, failed attempts); Test evidence (go test ./... -v -count=1 summary plus
   the full make verify output); Manual verification (anything the user
   reported, otherwise "none required"); Known limitations; Handoff to next
   phase; Metadata (commit hash, tag, branch).

3. Update .cline/memory-bank/progress.md with a one-line status.

4. Commit, then run: git tag -f phase-N-done

5. Fast-forward main:
   git -C /home/vivek/Documents/ClineAiHackathon merge --ff-only <current branch>.
   If it fails, STOP and report. Never force, rebase, or reset.

6. Rebuild the deployed binary:
   make -C /home/vivek/Documents/ClineAiHackathon build

7. Check the deployed binary: pipe
   {"jsonrpc":"2.0","id":1,"method":"tools/list"} into
   /home/vivek/Documents/ClineAiHackathon/bin/vault with VAULT_ROOT set to a
   temp dir, and print the tool names.

8. Reload the MCP server:
   - Find Cline's MCP settings file (expected at
     /home/vivek/.cline/data/settings/cline_mcp_settings.json; search ~/.cline if
     it is not there) and copy it to <file>.bak.
   - In the "vault" entry ONLY, make sure
     command=/home/vivek/Documents/ClineAiHackathon/bin/vault,
     env VAULT_ROOT=/home/vivek/Documents/ClineAiHackathon, disabled=false, and
     autoApprove lists every tool name from step 7.
   - Validate the file with python3 -m json.tool. Never modify any other server
     entry.

9. Back up the logs: find Cline's task history directory under ~/.cline (use ls;
   do not guess), copy it to ~/cline-log-backups/phase-N-<yyyymmdd-hhmm>/, and
   report the path and du -sh.

10. Finish with a section titled "USER ACTION REQUIRED" that lists only what you
    could not do yourself, or says "none".

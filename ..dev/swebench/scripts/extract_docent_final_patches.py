#!/usr/bin/env python3
"""Extract strict final SWE-bench predictions from Docent transcripts.

The extractor intentionally avoids "best/longest diff" heuristics. It accepts a
patch only when the transcript exposes either the final submission command or a
complete patch file printout. Partial inspection commands such as head/tail/sed
are rejected so the output can be used for verifier alignment checks.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
import time
import urllib.error
import urllib.request
from collections import Counter
from pathlib import Path
from typing import Any


BAD_COMMAND_MARKERS = (
    "head ",
    "tail ",
    "sed -n",
    "git apply",
    "stash",
)


def load_json(path: Path) -> Any:
    with path.open() as f:
        return json.load(f)


def dump_json(path: Path, data: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w") as f:
        json.dump(data, f, ensure_ascii=False, indent=2, sort_keys=True)
        f.write("\n")


def fetch_agent_run(collection_id: str, agent_run_id: str, timeout: int, retries: int) -> dict[str, Any]:
    url = f"https://api.docent.transluce.org/rest/{collection_id}/agent_run?agent_run_id={agent_run_id}"
    last_error: Exception | None = None
    for attempt in range(retries + 1):
        try:
            with urllib.request.urlopen(url, timeout=timeout) as resp:
                return json.load(resp)
        except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as exc:
            last_error = exc
            if attempt < retries:
                time.sleep(min(2**attempt, 10))
    raise RuntimeError(f"failed to fetch {agent_run_id}: {last_error}")


def command_from_tool_call(call: dict[str, Any]) -> str:
    arguments = call.get("arguments") or {}
    if isinstance(arguments, dict):
        command = arguments.get("command")
        return command if isinstance(command, str) else ""
    return ""


def iter_command_outputs(agent_run: dict[str, Any]) -> list[tuple[str, str]]:
    pairs: list[tuple[str, str]] = []
    for transcript in agent_run.get("transcripts") or []:
        pending: list[str] = []
        for message in transcript.get("messages") or []:
            for call in message.get("tool_calls") or []:
                command = command_from_tool_call(call)
                if command:
                    pending.append(command)
            if message.get("role") == "tool":
                command = pending.pop(0) if pending else ""
                content = message.get("content") or ""
                if command or content:
                    pairs.append((command, content))
    return pairs


def extract_unified_diff(text: str) -> str:
    lines = text.replace("\r\n", "\n").replace("\r", "\n").splitlines()
    start = next((idx for idx, line in enumerate(lines) if line.startswith("diff --git ")), None)
    if start is None:
        return ""
    patch = "\n".join(lines[start:]).rstrip()
    return patch + "\n" if patch else ""


def is_complete_patch_print(command: str) -> bool:
    command_lower = command.lower()
    if "patch.txt" not in command_lower or "cat" not in command_lower:
        return False
    return not any(marker in command_lower for marker in BAD_COMMAND_MARKERS)


def extract_patch(agent_run: dict[str, Any]) -> tuple[str, dict[str, Any]]:
    candidates: list[dict[str, Any]] = []
    for command, output in iter_command_outputs(agent_run):
        patch = extract_unified_diff(output)
        if not patch:
            continue
        if "COMPLETE_TASK_AND_SUBMIT_FINAL_OUTPUT" in output:
            candidates.append(
                {
                    "source": "final_submit",
                    "command": command,
                    "patch": patch,
                }
            )
        elif is_complete_patch_print(command):
            candidates.append(
                {
                    "source": "full_patch_file",
                    "command": command,
                    "patch": patch,
                }
            )

    if not candidates:
        return "", {"source": "missing_final_patch", "best_cmd": "", "patch_sha256": "", "patch_lines": 0}

    final_candidates = [item for item in candidates if item["source"] == "final_submit"]
    best = (final_candidates or candidates)[-1]
    patch = best["patch"]
    return patch, {
        "source": best["source"],
        "best_cmd": best["command"],
        "patch_sha256": hashlib.sha256(patch.encode()).hexdigest(),
        "patch_lines": len(patch.splitlines()),
    }


def manifest_items(path: Path) -> list[dict[str, Any]]:
    data = load_json(path)
    if isinstance(data, list):
        return data
    if isinstance(data, dict):
        return list(data.values())
    raise TypeError(f"unsupported manifest format: {type(data).__name__}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--collection-id", required=True, help="Docent collection id")
    parser.add_argument("--manifest", required=True, type=Path, help="JSON file with instance_id and agent_run_id")
    parser.add_argument("--output", required=True, type=Path, help="Output SWE-bench predictions JSON path")
    parser.add_argument("--meta-output", type=Path, help="Output extraction metadata JSON path")
    parser.add_argument("--limit", type=int, default=0, help="Only process the first N manifest rows")
    parser.add_argument("--timeout", type=int, default=60)
    parser.add_argument("--retries", type=int, default=3)
    parser.add_argument("--sleep", type=float, default=0.0, help="Delay between API calls")
    args = parser.parse_args()

    predictions: dict[str, dict[str, str]] = {}
    metadata: dict[str, dict[str, Any]] = {}
    counts: Counter[str] = Counter()

    rows = manifest_items(args.manifest)
    if args.limit > 0:
        rows = rows[: args.limit]

    for index, row in enumerate(rows, start=1):
        instance_id = row.get("instance_id") or row.get("metadata", {}).get("instance_id")
        agent_run_id = row.get("agent_run_id")
        if not instance_id or not agent_run_id:
            raise ValueError(f"manifest row {index} missing instance_id or agent_run_id")

        agent_run = fetch_agent_run(args.collection_id, agent_run_id, args.timeout, args.retries)
        patch, meta = extract_patch(agent_run)
        predictions[instance_id] = {
            "instance_id": instance_id,
            "model_name_or_path": "docent-official-final-patch",
            "model_patch": patch,
        }
        meta.update(
            {
                "agent_run_id": agent_run_id,
                "official_resolved": row.get("official_resolved"),
            }
        )
        metadata[instance_id] = meta
        counts[meta["source"]] += 1

        print(f"[{index}/{len(rows)}] {instance_id} {meta['source']} lines={meta['patch_lines']}", file=sys.stderr)
        if args.sleep > 0:
            time.sleep(args.sleep)

    dump_json(args.output, predictions)
    if args.meta_output:
        dump_json(args.meta_output, metadata)

    print(
        json.dumps(
            {
                "pred_count": len(predictions),
                "source_counts": dict(counts),
                "empty": sum(1 for item in predictions.values() if not item["model_patch"]),
                "sha256": hashlib.sha256(json.dumps(predictions, sort_keys=True).encode()).hexdigest(),
            },
            ensure_ascii=False,
            indent=2,
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

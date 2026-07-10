#!/usr/bin/env python3
"""Compare extracted Docent predictions against current local verifier results."""

from __future__ import annotations

import collections
import os
import hashlib
import json
from pathlib import Path
from typing import Any


ROOT = Path("/data/swebench-verified")
RAW = ROOT / "results/docent-official-patch-mock/raw"
DEFAULT_REPORT_PATH = (
    ROOT
    / "results/docent-official-patch-mock/local-harness-report/current-calibrated-20260710/"
    / "docent__minimax-m2.5-official-final-patch.docent-preds-current-calibrated-20260710-baseline.json"
)
OFFICIAL_PATH = (
    ROOT
    / "results/baseline-full/m2.5-high-full-500-mini210-clean-upstream/"
    / "docent_official_per_case_comparison.json"
)
DEFAULT_PREDS_PATH = RAW / "preds.json"
META_PATHS = [RAW / "preds_transcript_best.meta.json", RAW / "preds_strict_final.meta.json"]
DEFAULT_OUT_PREFIX = "current_calibrated_vs_docent_official_20260710"


def load_json(path: Path) -> Any:
    with path.open() as f:
        return json.load(f)


def rows_from_any(data: Any) -> list[dict[str, Any]]:
    if isinstance(data, list):
        return [row for row in data if isinstance(row, dict)]
    if isinstance(data, dict):
        if all(isinstance(v, dict) and "instance_id" in v for v in data.values()):
            return list(data.values())
        for key in ("rows", "cases", "items", "comparison"):
            if isinstance(data.get(key), list):
                return [row for row in data[key] if isinstance(row, dict)]
    return []


def idset(report: dict[str, Any], key: str) -> set[str]:
    return set(report.get(key) or [])


def main() -> int:
    report_path = Path(os.environ.get("REPORT_PATH", str(DEFAULT_REPORT_PATH)))
    preds_path = Path(os.environ.get("PREDS_PATH", str(DEFAULT_PREDS_PATH)))
    out_prefix = os.environ.get("OUT_PREFIX", DEFAULT_OUT_PREFIX)
    out_json = RAW / f"{out_prefix}.json"
    out_md = RAW / f"{out_prefix}.md"

    report = load_json(report_path)
    official_raw = load_json(OFFICIAL_PATH)
    preds = load_json(preds_path)

    metas: dict[str, dict[str, Any]] = {}
    for meta_path in META_PATHS:
        if not meta_path.exists():
            continue
        data = load_json(meta_path)
        for instance_id, value in data.items():
            metas.setdefault(instance_id, {})[meta_path.stem] = value

    official = {}
    for row in rows_from_any(official_raw):
        instance_id = row["instance_id"]
        if "docent_official_resolved" in row:
            official[instance_id] = bool(row["docent_official_resolved"])
        else:
            official[instance_id] = bool(row.get("official_resolved"))

    resolved = idset(report, "resolved_ids")
    unresolved = idset(report, "unresolved_ids")
    empty = idset(report, "empty_patch_ids")
    errors = idset(report, "error_ids")

    def local_status(instance_id: str) -> str:
        if instance_id in resolved:
            return "resolved"
        if instance_id in unresolved:
            return "unresolved"
        if instance_id in empty:
            return "empty_patch"
        if instance_id in errors:
            return "error"
        return "missing"

    rows: list[dict[str, Any]] = []
    for instance_id in sorted(official):
        patch = ""
        if isinstance(preds, dict):
            patch = (preds.get(instance_id) or {}).get("model_patch", "")
        status = local_status(instance_id)
        local_resolved = instance_id in resolved
        row: dict[str, Any] = {
            "instance_id": instance_id,
            "official_resolved": official[instance_id],
            "local_status": status,
            "local_resolved": local_resolved,
            "same_resolved_bool": official[instance_id] == local_resolved,
            "patch_empty": not bool(str(patch).strip()),
            "patch_sha256": hashlib.sha256(patch.encode()).hexdigest() if patch else "",
            "patch_lines": len(patch.splitlines()) if patch else 0,
        }
        for source_name, value in metas.get(instance_id, {}).items():
            if isinstance(value, dict):
                row[source_name + "_source"] = value.get("source")
                row[source_name + "_best_cmd"] = value.get("best_cmd")
                row[source_name + "_patch_lines"] = value.get("patch_lines") or value.get("best_len")

        if status == "error":
            log_path = (
                ROOT
                / "logs/run_evaluation/docent-preds-current-calibrated-20260710-baseline/"
                / "docent__minimax-m2.5-official-final-patch"
                / instance_id
                / "run_instance.log"
            )
            if log_path.exists():
                text = log_path.read_text(errors="replace")
                lines = [
                    line
                    for line in text.splitlines()
                    if "Patch Apply Failed" in line
                    or "unexpected" in line
                    or "malformed patch" in line
                ]
                row["error_excerpt"] = " | ".join(lines[:5])

        if row["same_resolved_bool"]:
            row["classification"] = "aligned"
        elif status == "empty_patch":
            row["classification"] = "docent_patch_not_exposed_or_empty"
        elif status == "error":
            row["classification"] = "extracted_patch_apply_failed"
        elif row["official_resolved"] and not row["local_resolved"]:
            row["classification"] = "official_resolved_local_unresolved"
        elif (not row["official_resolved"]) and row["local_resolved"]:
            row["classification"] = "official_unresolved_local_resolved"
        else:
            row["classification"] = "other"
        rows.append(row)

    matrix = collections.Counter((row["official_resolved"], row["local_status"]) for row in rows)
    summary = {
        "report_path": str(report_path),
        "preds_path": str(preds_path),
        "official_path": str(OFFICIAL_PATH),
        "total": len(rows),
        "official_resolved": sum(1 for row in rows if row["official_resolved"]),
        "local_resolved": sum(1 for row in rows if row["local_resolved"]),
        "same_resolved_bool": sum(1 for row in rows if row["same_resolved_bool"]),
        "diff_resolved_bool": sum(1 for row in rows if not row["same_resolved_bool"]),
        "local_status_counts": dict(collections.Counter(row["local_status"] for row in rows)),
        "classification_counts": dict(collections.Counter(row["classification"] for row in rows)),
        "matrix": {
            f"official={key[0]} local={key[1]}": value
            for key, value in sorted(matrix.items(), key=lambda item: str(item[0]))
        },
    }

    result = {"summary": summary, "rows": rows}
    out_json.write_text(json.dumps(result, ensure_ascii=False, indent=2, sort_keys=True) + "\n")

    with out_md.open("w") as f:
        f.write("# Docent Official Patch vs Current Calibrated Verifier\n\n")
        f.write(f"- Predictions: `{preds_path}`\n")
        f.write(f"- Harness report: `{report_path}`\n")
        f.write(f"- Total: {summary['total']}\n")
        f.write(f"- Official resolved: {summary['official_resolved']}\n")
        f.write(f"- Local resolved: {summary['local_resolved']}\n")
        f.write(f"- Same resolved bool: {summary['same_resolved_bool']}\n")
        f.write(f"- Diff resolved bool: {summary['diff_resolved_bool']}\n")
        f.write(f"- Local statuses: {summary['local_status_counts']}\n")
        f.write(f"- Classifications: {summary['classification_counts']}\n\n")
        f.write("## Discrepancies\n\n")
        f.write("| instance_id | official | local_status | patch_lines | classification | source | note |\n")
        f.write("|---|---:|---|---:|---|---|---|\n")
        for row in rows:
            if row["same_resolved_bool"]:
                continue
            source = row.get("preds_transcript_best.meta_source") or row.get(
                "preds_strict_final.meta_source"
            ) or ""
            note = row.get("error_excerpt") or row.get("preds_transcript_best.meta_best_cmd") or ""
            note = str(note).replace("|", "\\|")[:240]
            f.write(
                f"| {row['instance_id']} | {row['official_resolved']} | "
                f"{row['local_status']} | {row['patch_lines']} | {row['classification']} | "
                f"{source} | {note} |\n"
            )

    print(json.dumps(summary, ensure_ascii=False, indent=2, sort_keys=True))
    print(f"OUT_JSON {out_json}")
    print(f"OUT_MD {out_md}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

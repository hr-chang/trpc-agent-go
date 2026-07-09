#!/usr/bin/env python3
"""Audit Docent/local verifier discrepancies for exposed final patches."""

from __future__ import annotations

import argparse
import json
import subprocess
from pathlib import Path
from typing import Any


MODEL_NAME = "docent__minimax-m2.5-official-final-patch"


def load_json(path: Path) -> Any:
    with path.open() as f:
        return json.load(f)


def git_output(repo: Path, *args: str) -> str:
    return subprocess.check_output(["git", "-C", str(repo), *args], text=True)


def load_case(logs_root: Path, run_id: str, instance_id: str) -> dict[str, Any] | None:
    report_path = logs_root / run_id / MODEL_NAME / instance_id / "report.json"
    if not report_path.exists():
        return None
    data = load_json(report_path)[instance_id]
    tests_status = data.get("tests_status", {})

    def bucket(name: str) -> dict[str, Any]:
        value = tests_status.get(name, {})
        failures = value.get("failure", [])
        return {
            "success_count": len(value.get("success", [])),
            "failure_count": len(failures),
            "failures": failures[:20],
        }

    return {
        "resolved": data.get("resolved"),
        "patch_successfully_applied": data.get("patch_successfully_applied"),
        "FAIL_TO_PASS": bucket("FAIL_TO_PASS"),
        "PASS_TO_PASS": bucket("PASS_TO_PASS"),
        "log_dir": str(report_path.parent),
    }


def classify(instance_id: str, official: bool, clean: dict[str, Any] | None, calibrated: dict[str, Any] | None) -> tuple[str, str]:
    if not clean or not calibrated:
        return "needs_manual_review", "missing clean or calibrated per-case report."
    if clean["resolved"] != calibrated["resolved"]:
        if instance_id == "astropy__astropy-7606":
            return (
                "local_harness_modified_behavior",
                "calibrated astropy log parser maps parametrized pytest names like [unit0] back to []; clean upstream leaves PASS_TO_PASS failure test_compose_roundtrip[].",
            )
        if instance_id in {"astropy__astropy-8707", "astropy__astropy-8872"}:
            return (
                "local_harness_modified_behavior",
                "calibrated astropy 3.1 runtime pin adds pytest==6.2.5 and setuptools==59.8.0 in eval.sh; clean upstream fails relevant FTP/PTP tests.",
            )
        if instance_id == "django__django-10097":
            return (
                "local_harness_modified_behavior",
                "calibrated Django 2.2 sqlite legacy_alter_table sitecustomize shim is injected in eval.sh; clean upstream has FTP/PTP failures.",
            )
        return "local_harness_modified_behavior", "current calibrated harness differs from clean upstream."
    if clean["resolved"] != official:
        return (
            "official_scoring_or_record_drift",
            "clean upstream and calibrated local harness both resolve while Docent marks unresolved; local modifications are not the cause.",
        )
    return "no_discrepancy_after_clean_check", "clean/current/official are aligned."


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path("."))
    parser.add_argument("--raw-dir", type=Path, required=True)
    parser.add_argument("--clean-run-id", required=True)
    parser.add_argument("--calibrated-run-id", required=True)
    parser.add_argument("--current-repo", type=Path, required=True)
    parser.add_argument("--clean-repo", type=Path, required=True)
    parser.add_argument("--output-json", type=Path, required=True)
    parser.add_argument("--output-md", type=Path, required=True)
    parser.add_argument("--harness-diff-output", type=Path, required=True)
    args = parser.parse_args()

    root = args.root.resolve()
    raw_dir = (root / args.raw_dir).resolve()
    logs_root = root / "logs" / "run_evaluation"
    current_repo = (root / args.current_repo).resolve()
    clean_repo = (root / args.clean_repo).resolve()

    discrepancies = load_json(raw_dir / "discrepancies_full_patch_diff8.json")
    metadata = load_json(raw_dir / "preds_strict_final.meta.json")

    current_patch = git_output(current_repo, "diff", "--", "swebench/harness")
    harness_diff_output = (root / args.harness_diff_output).resolve()
    harness_diff_output.parent.mkdir(parents=True, exist_ok=True)
    harness_diff_output.write_text(current_patch)

    rows: list[dict[str, Any]] = []
    for item in sorted(discrepancies, key=lambda value: value["instance_id"]):
        instance_id = item["instance_id"]
        official = bool(item["official_resolved"])
        clean = load_case(logs_root, args.clean_run_id, instance_id)
        calibrated = load_case(logs_root, args.calibrated_run_id, instance_id)
        classification, cause = classify(instance_id, official, clean, calibrated)
        meta = metadata.get(instance_id, {})
        rows.append(
            {
                "instance_id": instance_id,
                "official_resolved": official,
                "clean_upstream_resolved": clean.get("resolved") if clean else None,
                "calibrated_resolved": calibrated.get("resolved") if calibrated else None,
                "classification": classification,
                "cause": cause,
                "patch_sha256": meta.get("patch_sha256", ""),
                "patch_lines": meta.get("patch_lines"),
                "strict_source": meta.get("source"),
                "best_cmd": meta.get("best_cmd"),
                "clean_summary": clean,
                "calibrated_summary": calibrated,
            }
        )

    audit = {
        "scope": "Docent full-final-patch discrepancies where official_resolved differs from local calibrated result",
        "clean_run_id": args.clean_run_id,
        "calibrated_run_id": args.calibrated_run_id,
        "current_swebench_head": git_output(current_repo, "rev-parse", "HEAD").strip(),
        "clean_swebench_head": git_output(clean_repo, "rev-parse", "HEAD").strip(),
        "clean_status_short": git_output(clean_repo, "status", "--short"),
        "current_harness_diff_stat": git_output(current_repo, "diff", "--stat"),
        "current_harness_diff_path": str(args.harness_diff_output),
        "rows": rows,
    }

    output_json = (root / args.output_json).resolve()
    output_md = (root / args.output_md).resolve()
    output_json.parent.mkdir(parents=True, exist_ok=True)
    with output_json.open("w") as f:
        json.dump(audit, f, ensure_ascii=False, indent=2)
        f.write("\n")

    with output_md.open("w") as f:
        f.write("# Verifier Discrepancy Audit\n\n")
        f.write(f"- Clean upstream run: `{args.clean_run_id}`\n")
        f.write(f"- Calibrated run: `{args.calibrated_run_id}`\n")
        f.write(f"- SWE-bench HEAD: `{audit['current_swebench_head']}`\n")
        f.write(f"- Clean status: `{audit['clean_status_short'].strip() or 'clean'}`\n")
        f.write(f"- Current harness diff: `{args.harness_diff_output}`\n\n")
        f.write("| instance_id | official | clean | calibrated | classification | cause | key clean failures |\n")
        f.write("|---|---:|---:|---:|---|---|---|\n")
        for row in rows:
            clean = row["clean_summary"] or {}
            failures: list[str] = []
            for bucket_name in ("FAIL_TO_PASS", "PASS_TO_PASS"):
                bucket = clean.get(bucket_name, {})
                if bucket.get("failure_count"):
                    sample = "; ".join(bucket.get("failures", [])[:3])
                    failures.append(f"{bucket_name}:{bucket['failure_count']} {sample}")
            failure_text = "<br>".join(failures) if failures else "none"
            f.write(
                f"| {row['instance_id']} | {row['official_resolved']} | "
                f"{row['clean_upstream_resolved']} | {row['calibrated_resolved']} | "
                f"{row['classification']} | {row['cause']} | {failure_text} |\n"
            )

    print(json.dumps({"output_json": str(output_json), "output_md": str(output_md), "rows": rows}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

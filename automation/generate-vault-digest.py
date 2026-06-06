#!/usr/bin/env python3
"""Generate a daily digest of vault activity for the previous working day.

Scans the vault for Markdown notes modified within a working-day window and
prepends a dated section to a rolling digest note. Run on weekdays: a Monday
run covers Friday/Saturday/Sunday, every other weekday covers the prior day.

Vault-agnostic: every path is relative to --vault-root and overridable. Shipped
in the hebb binary and materialised by `hebb install`; scheduled via launchd
(local.hebb.<vault>.daily-digest). The companion run-vault-digest.sh runs this
then reindexes with `hebb index`.
"""

from __future__ import annotations

import argparse
import datetime as dt
from dataclasses import dataclass
from pathlib import Path


DEFAULT_OUTPUT = "2-Areas/_DAILY-DIGEST.md"
MAX_ENTRIES = 30

# Directory names anywhere in the path that should never be scanned. Dotted
# dirs (.hebb, .obsidian, .trash, ...) are already skipped by is_excluded.
EXCLUDE_DIRS = {
    "assets",
}

# Auto-generated / system notes that would otherwise be daily noise.
EXCLUDE_BASENAMES = {
    "_DAILY-DIGEST.md",
    "_ACTION-REVIEW.md",
    "_INGEST-LOG.md",
}

# Top-level folders shown first and in this order; everything else follows.
PARA_ORDER = ["1-Projects", "2-Areas", "3-Resources", "4-Archives"]


@dataclass(frozen=True)
class Touched:
    path: Path  # relative to vault root
    title: str
    modified: dt.datetime
    is_new: bool

    @property
    def group(self) -> str:
        parts = self.path.parts
        return parts[0] if len(parts) > 1 else "Vault root"


def compute_window(today: dt.date) -> tuple[dt.datetime, dt.datetime, str]:
    """Return (start, end, label) for the activity window.

    Monday covers the preceding Fri/Sat/Sun; any other day covers the single
    prior calendar day. The window is [start, end) in local time.
    """
    end = dt.datetime.combine(today, dt.time.min)
    if today.weekday() == 0:  # Monday
        start_date = today - dt.timedelta(days=3)  # Friday
    else:
        start_date = today - dt.timedelta(days=1)
    start = dt.datetime.combine(start_date, dt.time.min)

    if start_date == today - dt.timedelta(days=1):
        label = f"{start_date.isoformat()} ({start_date.strftime('%a')})"
    else:
        last_day = today - dt.timedelta(days=1)
        label = (
            f"{start_date.isoformat()} ({start_date.strftime('%a')}) "
            f"to {last_day.isoformat()} ({last_day.strftime('%a')})"
        )
    return start, end, label


def is_excluded(path: Path) -> bool:
    if any(part in EXCLUDE_DIRS for part in path.parts):
        return True
    if any(part.startswith(".") for part in path.parts):
        return True
    return path.name in EXCLUDE_BASENAMES


def extract_title(path: Path) -> str:
    try:
        for line in path.read_text(encoding="utf-8").splitlines():
            if line.startswith("# "):
                return line.removeprefix("# ").strip()
    except OSError:
        pass
    return path.stem


def wiki_link(rel_path: Path, label: str) -> str:
    without_suffix = str(rel_path.with_suffix(""))
    return f"[[{without_suffix}|{label}]]"


def collect_touched(
    vault_root: Path, start: dt.datetime, end: dt.datetime
) -> list[Touched]:
    start_ts, end_ts = start.timestamp(), end.timestamp()
    touched: list[Touched] = []

    for path in vault_root.rglob("*.md"):
        rel = path.relative_to(vault_root)
        if is_excluded(rel):
            continue
        try:
            stat = path.stat()
        except OSError:
            continue
        if not (start_ts <= stat.st_mtime < end_ts):
            continue
        birth = getattr(stat, "st_birthtime", stat.st_mtime)
        touched.append(
            Touched(
                path=rel,
                title=extract_title(path),
                modified=dt.datetime.fromtimestamp(stat.st_mtime),
                is_new=start_ts <= birth < end_ts,
            )
        )

    return touched


def group_key(group: str) -> tuple[int, str]:
    if group in PARA_ORDER:
        return (PARA_ORDER.index(group), "")
    if group == "Vault root":
        return (len(PARA_ORDER) + 1, "")
    return (len(PARA_ORDER), group.lower())


def render_entry(
    today: dt.date, label: str, touched: list[Touched]
) -> str:
    heading = f"## {today.isoformat()} — activity for {label}"

    if not touched:
        return f"{heading}\n\n_No vault activity in this window._\n"

    groups: dict[str, list[Touched]] = {}
    for item in touched:
        groups.setdefault(item.group, []).append(item)

    lines = [heading, "", f"**Notes touched:** {len(touched)}", ""]
    for group in sorted(groups, key=group_key):
        items = sorted(groups[group], key=lambda i: i.modified)
        lines.append(f"### {group} ({len(items)})")
        for item in items:
            marker = "new" if item.is_new else "updated"
            stamp = item.modified.strftime("%Y-%m-%d %H:%M")
            lines.append(
                f"- {wiki_link(item.path, item.title)} — {marker}, {stamp}"
            )
        lines.append("")

    return "\n".join(lines).rstrip() + "\n"


HEADER = (
    "# Vault Daily Digest\n\n"
    "Automated digest of vault activity, newest first. "
    "Generated by hebb's `generate-vault-digest.py` on weekdays.\n"
)


def split_existing_entries(text: str) -> list[str]:
    marker = "\n## "
    idx = text.find(marker)
    if idx == -1:
        return []
    body = text[idx + 1 :]  # drop leading newline, keep "## ..."
    chunks = body.split("\n## ")
    entries = []
    for i, chunk in enumerate(chunks):
        chunk = chunk.strip()
        if not chunk:
            continue
        entries.append(chunk if i == 0 else "## " + chunk)
    return entries


def build_document(new_entry: str, output_path: Path, today: dt.date) -> str:
    existing = []
    if output_path.exists():
        existing = split_existing_entries(
            output_path.read_text(encoding="utf-8")
        )
    # Drop any prior entry for the same run date so reruns replace, not duplicate.
    same_day_prefix = f"## {today.isoformat()} "
    existing = [e for e in existing if not e.startswith(same_day_prefix)]
    entries = [new_entry.strip()] + existing
    entries = entries[:MAX_ENTRIES]
    return HEADER + "\n---\n\n" + "\n\n---\n\n".join(entries) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--vault-root", default=".", help="Path to the vault root")
    parser.add_argument("--output", default=DEFAULT_OUTPUT, help="Digest note path (relative to vault root)")
    parser.add_argument(
        "--date",
        default=None,
        help="Override run date as YYYY-MM-DD (for testing)",
    )
    args = parser.parse_args()

    vault_root = Path(args.vault_root).resolve()
    output_path = (vault_root / args.output).resolve()
    output_path.parent.mkdir(parents=True, exist_ok=True)

    today = (
        dt.date.fromisoformat(args.date) if args.date else dt.date.today()
    )
    start, end, label = compute_window(today)
    touched = collect_touched(vault_root, start, end)
    new_entry = render_entry(today, label, touched)
    output_path.write_text(
        build_document(new_entry, output_path, today), encoding="utf-8"
    )

    print(
        f"Wrote {output_path.relative_to(vault_root)} "
        f"({len(touched)} notes for {label})"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

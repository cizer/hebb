#!/usr/bin/env python3
"""Generate a vault-level action review from meeting action registers.

Scans the vault for action-register files (default: OPEN-ACTIONS.md), collates
the open actions, and writes a prioritised review note plus a machine-readable
JSON export. Vault-agnostic: every path is relative to --vault-root and the
register name, output paths, and the highlighted owner are overridable.

Shipped in the hebb binary and materialised by `hebb install`; scheduled via
launchd (local.hebb.<vault>.action-review).
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import re
import subprocess
from dataclasses import dataclass
from pathlib import Path


DEFAULT_OUTPUT = "2-Areas/_ACTION-REVIEW.md"
DEFAULT_JSON_OUTPUT = "2-Areas/_ACTION-REVIEW.json"
DEFAULT_REGISTER_NAME = "OPEN-ACTIONS.md"


def normalize_status(status: str) -> str:
    """Lower-case status with any leading emoji / symbols / whitespace stripped.

    Registers vary: some use plain `Open` / `Done`, others decorate the status
    (`🔴 Overdue`, `✅ Done`, `🟡 Needs review`). Normalise before comparing.
    """
    return re.sub(r"^[^a-z]+", "", status.lower()).strip()


@dataclass(frozen=True)
class Action:
    status: str
    action: str
    owner: str
    review_due: str
    first_raised: str
    latest_source: str
    register_path: Path
    register_title: str
    owner_filter: str = ""  # name to treat as "mine"; empty disables the match

    @property
    def date_value(self) -> dt.date | None:
        try:
            return dt.date.fromisoformat(self.review_due.strip())
        except ValueError:
            return None

    @property
    def is_mine(self) -> bool:
        # An empty owner filter must match nothing (not everything).
        return bool(self.owner_filter) and self.owner_filter.lower() in self.owner.lower()

    @property
    def is_overdue(self) -> bool:
        date_value = self.date_value
        return date_value is not None and date_value <= dt.date.today() and normalize_status(self.status) != "done"

    @property
    def stable_id(self) -> str:
        raw = "|".join(
            [
                str(self.register_path),
                self.first_raised,
                self.action.lower(),
                self.owner.lower(),
            ]
        )
        return hashlib.sha1(raw.encode("utf-8")).hexdigest()[:12]


def split_markdown_row(line: str) -> list[str]:
    stripped = line.strip()
    if not stripped.startswith("|") or not stripped.endswith("|"):
        return []
    return [cell.strip() for cell in stripped.strip("|").split("|")]


def extract_title(path: Path) -> str:
    try:
        for line in path.read_text(encoding="utf-8").splitlines():
            if line.startswith("# "):
                return line.removeprefix("# ").strip()
    except OSError:
        pass
    return path.parent.name


def parse_register(path: Path, vault_root: Path, owner_filter: str) -> list[Action]:
    actions: list[Action] = []
    rows_started = False
    title = extract_title(path)

    for line in path.read_text(encoding="utf-8").splitlines():
        cells = split_markdown_row(line)
        if not cells:
            if rows_started:
                break
            continue

        first = cells[0].lower()
        if first == "status":
            rows_started = True
            continue
        if set(cells[0].replace(" ", "")) <= {"-"}:
            continue
        if not rows_started or len(cells) < 6:
            continue

        actions.append(
            Action(
                status=cells[0],
                action=cells[1],
                owner=cells[2],
                review_due=cells[3],
                first_raised=cells[4],
                latest_source=cells[5],
                register_path=path.relative_to(vault_root),
                register_title=title,
                owner_filter=owner_filter,
            )
        )

    return actions


def wiki_link(path: Path, label: str | None = None) -> str:
    without_suffix = str(path.with_suffix(""))
    if label:
        return f"[[{without_suffix}|{label}]]"
    return f"[[{without_suffix}]]"


def markdown_table(actions: list[Action], vault_root: Path) -> str:
    if not actions:
        return "_None._\n"

    lines = [
        "| Status | Action | Owner | Review / Due | Register | Latest Source |",
        "| --- | --- | --- | --- | --- | --- |",
    ]

    for item in actions:
        register = wiki_link(item.register_path, item.register_title.replace("Open Actions - ", ""))
        lines.append(
            "| "
            + " | ".join(
                [
                    item.status,
                    item.action,
                    item.owner,
                    item.review_due,
                    register,
                    item.latest_source,
                ]
            )
            + " |"
        )

    return "\n".join(lines) + "\n"


def sort_actions(actions: list[Action]) -> list[Action]:
    status_rank = {
        "overdue": 0,
        "needs review": 1,
        "waiting": 2,
        "open": 3,
        "done": 9,
    }
    return sorted(
        actions,
        key=lambda item: (
            status_rank.get(item.status.lower(), 5),
            item.date_value or dt.date.max,
            item.register_title,
            item.action.lower(),
        ),
    )


def collect_actions(
    vault_root: Path, output_path: Path, register_name: str, owner_filter: str
) -> tuple[list[Path], list[Action]]:
    registers = sorted(
        path
        for path in vault_root.rglob(register_name)
        if "4-Archives" not in path.parts and path.resolve() != output_path.resolve()
    )

    actions = sort_actions(
        [
            action
            for register in registers
            for action in parse_register(register, vault_root, owner_filter)
            if normalize_status(action.status) != "done"
        ]
    )

    return registers, actions


def action_to_dict(action: Action) -> dict[str, str | bool | None]:
    return {
        "id": action.stable_id,
        "status": action.status,
        "action": action.action,
        "owner": action.owner,
        "review_due": action.review_due,
        "first_raised": action.first_raised,
        "latest_source": action.latest_source,
        "register_path": str(action.register_path),
        "register_title": action.register_title,
        "mine": action.is_mine,
        "overdue": action.is_overdue,
    }


def build_json(actions: list[Action], registers: list[Path], vault_root: Path) -> str:
    payload = {
        "generated": dt.date.today().isoformat(),
        "registers": [str(path.relative_to(vault_root)) for path in registers],
        "actions": [action_to_dict(action) for action in actions],
    }
    return json.dumps(payload, indent=2, ensure_ascii=False) + "\n"


def extract_priority_block(output_path: Path) -> str:
    default = """Use this section as the human decision point. The tables below show the work; this section chooses what actually gets attention.

- [ ]
- [ ]
- [ ]
"""
    if not output_path.exists():
        return default

    text = output_path.read_text(encoding="utf-8")
    for heading in ("## Priority List", "## Prioritise Today"):
        marker = f"{heading}\n\n"
        start = text.find(marker)
        if start == -1:
            continue
        block_start = start + len(marker)
        block_end = text.find("\n---", block_start)
        if block_end == -1:
            continue
        block = text[block_start:block_end].strip()
        if block:
            return block + "\n"

    return default


def build_review(
    vault_root: Path,
    output_path: Path,
    register_name: str,
    owner_filter: str,
    registers: list[Path],
    actions: list[Action],
) -> tuple[str, str]:
    today = dt.date.today()
    overdue = [item for item in actions if normalize_status(item.status) == "overdue" or item.is_overdue]
    mine = [item for item in actions if item.is_mine]
    waiting = [item for item in actions if normalize_status(item.status) == "waiting"]
    needs_review = [item for item in actions if normalize_status(item.status) == "needs review"]

    register_lines = "\n".join(f"- {wiki_link(path.relative_to(vault_root))}" for path in registers)
    priority_block = extract_priority_block(output_path)

    mine_heading = f"My Actions ({owner_filter})" if owner_filter else "My Actions"

    markdown = f"""# Action Review

Generated from all `{register_name}` files in the vault.

**Generated:** {today.isoformat()}
**Registers found:** {len(registers)}
**Open actions:** {len(actions)}

---

## Priority List

{priority_block}

---

## Overdue / Due Now

{markdown_table(overdue, vault_root)}
---

## {mine_heading}

{markdown_table(mine, vault_root)}
---

## Needs Review

{markdown_table(needs_review, vault_root)}
---

## Waiting On Others

{markdown_table(waiting, vault_root)}
---

## Registers Included

{register_lines if register_lines else "_No action registers found._"}

---

## External Todo Sync

Recommended sync boundary:

- Sync only items from `Priority List` and `My Actions`.
- Keep `Waiting On Others` in the vault unless it needs a reminder to chase.
- Keep meeting-level `{register_name}` files as the source of truth for meeting continuity.
- Store any external task ID back beside the action only if two-way sync becomes necessary.
- Use the `.json` export as the machine-readable feed for todo-system sync.

"""

    return markdown, build_json(actions, registers, vault_root)


def sort_by_due(actions: list[Action]) -> list[Action]:
    return sorted(
        actions,
        key=lambda item: (
            item.date_value or dt.date.max,
            item.register_title,
            item.action.lower(),
        ),
    )


def build_mine(
    vault_root: Path, register_name: str, owner_filter: str, actions: list[Action]
) -> str:
    """Personal worklist: only the owner's open actions, bucketed by urgency."""
    mine = [item for item in actions if item.is_mine]
    overdue = sort_by_due([item for item in mine if item.is_overdue or normalize_status(item.status) == "overdue"])
    rest = [item for item in mine if item not in overdue]
    waiting = sort_by_due([item for item in rest if normalize_status(item.status) == "waiting"])
    current = sort_by_due([item for item in rest if item not in waiting])

    return f"""# My Open Actions

Personal worklist for **{owner_filter}**, generated from all `{register_name}` files in the vault.

**Generated:** {dt.date.today().isoformat()}
**Open actions:** {len(mine)} open ({len(overdue)} overdue, {len(current)} current, {len(waiting)} waiting)

---

## Overdue

{markdown_table(overdue, vault_root)}
---

## Current

{markdown_table(current, vault_root)}
---

## Waiting

{markdown_table(waiting, vault_root)}
"""


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--vault-root", default=".", help="Path to the vault root")
    parser.add_argument("--output", default=DEFAULT_OUTPUT, help="Output markdown file (relative to vault root)")
    parser.add_argument("--json-output", default=DEFAULT_JSON_OUTPUT, help="Output JSON file (relative to vault root)")
    parser.add_argument("--register-name", default=DEFAULT_REGISTER_NAME, help="Action-register filename to scan for")
    parser.add_argument("--owner", default="", help="Owner name to highlight under 'My Actions' (matched as a substring; empty disables)")
    parser.add_argument(
        "--mine-output",
        default="",
        help="Also write a personal worklist of the owner's actions to this file (relative to vault root; empty disables; requires --owner)",
    )
    args = parser.parse_args()

    if args.mine_output and not args.owner:
        parser.error("--mine-output requires --owner")

    vault_root = Path(args.vault_root).resolve()
    output_path = (vault_root / args.output).resolve()
    json_output_path = (vault_root / args.json_output).resolve()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    json_output_path.parent.mkdir(parents=True, exist_ok=True)
    registers, actions = collect_actions(vault_root, output_path, args.register_name, args.owner)
    markdown, json_export = build_review(vault_root, output_path, args.register_name, args.owner, registers, actions)
    output_path.write_text(markdown, encoding="utf-8")
    json_output_path.write_text(json_export, encoding="utf-8")
    print(f"Wrote {display_path(output_path, vault_root)}")
    print(f"Wrote {display_path(json_output_path, vault_root)}")
    if args.mine_output:
        mine_path = (vault_root / args.mine_output).resolve()
        mine_path.parent.mkdir(parents=True, exist_ok=True)
        mine_path.write_text(build_mine(vault_root, args.register_name, args.owner, actions), encoding="utf-8")
        print(f"Wrote {display_path(mine_path, vault_root)}")

    # Headless notification: shell out to `hebb notify` so the script gains no
    # HTTP dependency. Completely optional: skipped when $HEBB_BIN is absent and
    # hebb is not on PATH, or when the binary exits non-zero (notify disabled or
    # no URL configured). Delivery failure never blocks or fails the note write.
    open_count = sum(1 for a in actions if normalize_status(a.status) != "done")
    _notify_action_review(str(display_path(output_path, vault_root)), open_count, args.vault_root)

    return 0


def _notify_action_review(note_path: str, open_count: int, vault_root: str) -> None:
    """Shell out to `hebb notify` with a one-line action-review summary.

    Reads $HEBB_BIN for the binary path (injected by the rendered launchd job
    as the pinned absolute path); falls back to the bare 'hebb' name so it
    works when hebb is on PATH. If the binary is absent or the notify config is
    not enabled/has no URL, hebb notify exits non-zero and we log and continue.
    The note path and count are the only content; no HTTP dependency is added.
    """
    hebb_bin = os.environ.get("HEBB_BIN", "hebb")
    summary = f"action-review: {open_count} open actions ({note_path})"
    try:
        result = subprocess.run(
            [hebb_bin, "--vault", vault_root, "notify", summary],
            capture_output=True,
            timeout=35,
        )
        if result.returncode != 0:
            # Not an error: notify may be disabled or have no URL configured.
            pass
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        # hebb not on PATH or timed out: silently skip notification.
        pass


def display_path(path: Path, vault_root: Path) -> Path | str:
    """Path relative to the vault for readable logs, or the absolute path when
    an output flag points outside the vault (relative_to would otherwise raise)."""
    try:
        return path.relative_to(vault_root)
    except ValueError:
        return path


if __name__ == "__main__":
    raise SystemExit(main())

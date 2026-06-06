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
from dataclasses import dataclass
from pathlib import Path


DEFAULT_OUTPUT = "2-Areas/_ACTION-REVIEW.md"
DEFAULT_JSON_OUTPUT = "2-Areas/_ACTION-REVIEW.json"
DEFAULT_REGISTER_NAME = "OPEN-ACTIONS.md"


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
        return date_value is not None and date_value <= dt.date.today() and self.status.lower() != "done"

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
            if action.status.lower() != "done"
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
    vault_root: Path, output_path: Path, register_name: str, owner_filter: str
) -> tuple[str, str]:
    registers, actions = collect_actions(vault_root, output_path, register_name, owner_filter)

    today = dt.date.today()
    overdue = [item for item in actions if item.status.lower() == "overdue" or item.is_overdue]
    mine = [item for item in actions if item.is_mine]
    waiting = [item for item in actions if item.status.lower() == "waiting"]
    needs_review = [item for item in actions if item.status.lower() == "needs review"]

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


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--vault-root", default=".", help="Path to the vault root")
    parser.add_argument("--output", default=DEFAULT_OUTPUT, help="Output markdown file (relative to vault root)")
    parser.add_argument("--json-output", default=DEFAULT_JSON_OUTPUT, help="Output JSON file (relative to vault root)")
    parser.add_argument("--register-name", default=DEFAULT_REGISTER_NAME, help="Action-register filename to scan for")
    parser.add_argument("--owner", default="", help="Owner name to highlight under 'My Actions' (matched as a substring; empty disables)")
    args = parser.parse_args()

    vault_root = Path(args.vault_root).resolve()
    output_path = (vault_root / args.output).resolve()
    json_output_path = (vault_root / args.json_output).resolve()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    json_output_path.parent.mkdir(parents=True, exist_ok=True)
    markdown, json_export = build_review(vault_root, output_path, args.register_name, args.owner)
    output_path.write_text(markdown, encoding="utf-8")
    json_output_path.write_text(json_export, encoding="utf-8")
    print(f"Wrote {display_path(output_path, vault_root)}")
    print(f"Wrote {display_path(json_output_path, vault_root)}")
    return 0


def display_path(path: Path, vault_root: Path) -> Path | str:
    """Path relative to the vault for readable logs, or the absolute path when
    an output flag points outside the vault (relative_to would otherwise raise)."""
    try:
        return path.relative_to(vault_root)
    except ValueError:
        return path


if __name__ == "__main__":
    raise SystemExit(main())

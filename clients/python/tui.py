#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.12"
# dependencies = ["textual>=0.79", "httpx>=0.27"]
# ///
"""A terminal client for poking at every ClaudeBox endpoint.

    uv run clients/python/tui.py --url http://localhost:8091 --key cbx_live_...

Or let it find the key the way the box stores it:

    uv run clients/python/tui.py --key "$(ssh root@box cbx api-key show | cut -f2)"

Every request it makes is printed in the log at the bottom with its status and
how long it took, because the point of this client is to show the API working
rather than to hide it.
"""

from __future__ import annotations

import argparse
import json
import os
import threading
from pathlib import Path
from typing import Any

from textual import on, work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical, VerticalScroll
from textual.screen import ModalScreen
from textual.widgets import (
    Button,
    DataTable,
    Footer,
    Header,
    Input,
    Label,
    RichLog,
    Static,
    TabbedContent,
    TabPane,
    TextArea,
)

from claudebox import Answer, Call, ClaudeBox, ClaudeBoxError, Session, timestamped

# --------------------------------------------------------------------- modals


class NewSession(ModalScreen[dict[str, Any] | None]):
    """Everything a session is fixed with at creation.

    Model, effort and permission mode live here rather than on a query
    because Claude Code treats all three as properties of a session.
    """

    BINDINGS = [Binding("escape", "dismiss(None)", "Cancel")]

    def compose(self) -> ComposeResult:
        with Vertical(id="dialog"):
            yield Label("New session", classes="dialog-title")
            yield Input(placeholder="name", id="name")
            yield Input(placeholder="repo (owner/repo, optional)", id="repo")
            yield Input(placeholder="system prompt (optional)", id="system_prompt")
            yield Input(placeholder="model — opus, sonnet, opus[1m] (optional)", id="model")
            yield Input(placeholder="effort — low medium high xhigh max (optional)", id="effort")
            yield Input(
                placeholder="permission mode — acceptEdits auto bypassPermissions manual",
                id="permission_mode",
            )
            yield Input(placeholder="skills to invoke, comma separated (costs a turn each)", id="skills")
            yield Label(
                "Skills on the box already load for every session — name them "
                "here only to actually run them.",
                classes="hint",
            )
            with Horizontal(classes="dialog-buttons"):
                yield Button("Create", variant="primary", id="create")
                yield Button("Cancel", id="cancel")

    def on_mount(self) -> None:
        self.query_one("#name", Input).focus()

    @on(Button.Pressed, "#cancel")
    def cancel(self) -> None:
        self.dismiss(None)

    @on(Button.Pressed, "#create")
    @on(Input.Submitted)
    def create(self) -> None:
        def value(field: str) -> str:
            return self.query_one(f"#{field}", Input).value.strip()

        name = value("name")
        if not name:
            self.query_one("#name", Input).focus()
            return
        skills = [s.strip() for s in value("skills").split(",") if s.strip()]
        self.dismiss(
            {
                "name": name,
                "repo": value("repo"),
                "system_prompt": value("system_prompt"),
                "model": value("model"),
                "effort": value("effort"),
                "permission_mode": value("permission_mode"),
                "skills": skills,
                "respond_within": "5m" if skills else None,
            }
        )


class AskFor(ModalScreen[str | None]):
    """One line of input, for the endpoints that need exactly one."""

    BINDINGS = [Binding("escape", "dismiss(None)", "Cancel")]

    def __init__(self, title: str, placeholder: str, hint: str = "", value: str = ""):
        super().__init__()
        self._title, self._placeholder, self._hint, self._value = title, placeholder, hint, value

    def compose(self) -> ComposeResult:
        with Vertical(id="dialog"):
            yield Label(self._title, classes="dialog-title")
            yield Input(placeholder=self._placeholder, value=self._value, id="value")
            if self._hint:
                yield Label(self._hint, classes="hint")
            with Horizontal(classes="dialog-buttons"):
                yield Button("OK", variant="primary", id="ok")
                yield Button("Cancel", id="cancel")

    def on_mount(self) -> None:
        self.query_one("#value", Input).focus()

    @on(Button.Pressed, "#cancel")
    def cancel(self) -> None:
        self.dismiss(None)

    @on(Button.Pressed, "#ok")
    @on(Input.Submitted)
    def ok(self) -> None:
        self.dismiss(self.query_one("#value", Input).value.strip())


class ShowText(ModalScreen[None]):
    """Read-only output — a spec, a fetched artifact, an OpenAPI document."""

    BINDINGS = [Binding("escape", "dismiss", "Close")]

    def __init__(self, title: str, body: str):
        super().__init__()
        self._title, self._body = title, body

    def compose(self) -> ComposeResult:
        with Vertical(id="viewer"):
            yield Label(self._title, classes="dialog-title")
            area = TextArea(self._body, read_only=True, soft_wrap=True)
            area.show_line_numbers = False
            yield area
            yield Label("esc to close", classes="hint")


# ------------------------------------------------------------------------ app


class ClaudeBoxTUI(App):
    CSS = """
    Screen { layout: vertical; }
    #body { height: 1fr; }
    #sessions { width: 38%; border-right: solid $panel-darken-2; }
    #detail { width: 1fr; }
    #log { height: 10; border-top: solid $panel-darken-2; padding: 0 1; }
    DataTable { height: 1fr; }
    .pane { padding: 1 2; height: 1fr; }
    .field-label { color: $text-muted; }
    #prompt { height: 5; border: solid $panel-darken-2; }
    #answer { height: 1fr; min-height: 8; border: solid $panel-darken-2; margin-top: 1; }
    #dialog {
        width: 74; padding: 1 2; background: $surface;
        border: thick $primary; height: auto;
    }
    #viewer {
        width: 90%; height: 80%; padding: 1 2;
        background: $surface; border: thick $primary;
    }
    .dialog-title { text-style: bold; margin-bottom: 1; }
    .dialog-buttons { height: auto; margin-top: 1; }
    .dialog-buttons Button { margin-right: 2; }
    .hint { color: $text-muted; margin-top: 1; width: 100%; }
    ModalScreen { align: center middle; }
    """

    BINDINGS = [
        Binding("n", "new_session", "New"),
        Binding("d", "delete_session", "Delete"),
        Binding("r", "refresh", "Refresh"),
        Binding("c", "run_command", "Command"),
        Binding("p", "system_prompt", "Prompt"),
        Binding("s", "put_skill", "Add skill"),
        Binding("m", "show_commands", "Allowlist"),
        Binding("o", "show_openapi", "OpenAPI"),
        Binding("k", "rotate_key", "Rotate key"),
        Binding("q", "quit", "Quit"),
    ]

    def __init__(self, box: ClaudeBox):
        super().__init__()
        self.box = box
        self.sessions: list[Session] = []
        self.selected: str | None = None
        self.last_job: str | None = None

    # ------------------------------------------------------------------ layout

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        with Horizontal(id="body"):
            with Vertical(id="sessions"):
                yield DataTable(id="session-table", cursor_type="row")
            with TabbedContent(id="detail"):
                with TabPane("Query", id="tab-query"):
                    with VerticalScroll(classes="pane"):
                        yield Label("prompt", classes="field-label")
                        yield TextArea(id="prompt", soft_wrap=True)
                        yield Label("respond_within — required, no default", classes="field-label")
                        yield Input(value="60s", id="respond_within")
                        yield Label("artifacts to declare, comma separated", classes="field-label")
                        with Horizontal(classes="dialog-buttons"):
                            yield Input(placeholder="report.html", id="artifacts")
                            yield Button("Stamp", id="stamp")
                        yield Label(
                            "Only declared files are fetchable. Stamp adds a UTC "
                            "timestamp so a re-run keeps the earlier report.",
                            classes="hint",
                        )
                        with Horizontal(classes="dialog-buttons"):
                            yield Button("Send", variant="primary", id="send")
                            yield Button("Poll job", id="poll")
                            yield Button("Cancel job", variant="error", id="cancel-job")
                        yield RichLog(id="answer", wrap=True, markup=True)
                with TabPane("Artifacts", id="tab-artifacts"):
                    with Vertical(classes="pane"):
                        yield DataTable(id="artifact-table", cursor_type="row")
                        with Horizontal(classes="dialog-buttons"):
                            yield Button("Fetch", variant="primary", id="fetch")
                            yield Button("Save to ./out", id="save")
                with TabPane("Session", id="tab-session"):
                    with VerticalScroll(classes="pane"):
                        yield Static(id="session-detail")
        yield RichLog(id="log", markup=True, max_lines=500)
        yield Footer()

    def on_mount(self) -> None:
        self.title = "ClaudeBox"
        self.sub_title = self.box.base_url
        table = self.query_one("#session-table", DataTable)
        table.add_columns("session", "kind", "status", "turns")
        arts = self.query_one("#artifact-table", DataTable)
        arts.add_columns("artifact", "size", "expires")
        self.check_health()
        self.action_refresh()

    # -------------------------------------------------------------- call log

    def log_call(self, call: Call) -> None:
        """Show one request. Called from worker threads, so it hops the loop."""
        colour = "green" if call.status < 400 else "red"
        note = f"  [dim]{call.note}[/dim]" if call.note else ""
        line = (
            f"[{colour}]{call.status}[/{colour}] "
            f"[bold]{call.method}[/bold] {call.path}  [dim]{call.ms}ms[/dim]{note}"
        )
        if threading.current_thread() is threading.main_thread():
            self._write_log(line)
        else:
            self.call_from_thread(self._write_log, line)

    def _write_log(self, line: str) -> None:
        self.query_one("#log", RichLog).write(line)

    def say(self, message: str, error: bool = False) -> None:
        self.query_one("#log", RichLog).write(
            f"[red]{message}[/red]" if error else f"[dim]{message}[/dim]"
        )

    def fail(self, err: Exception) -> None:
        if isinstance(err, ClaudeBoxError) and err.session_busy:
            self.say(f"{err}  — wait for it, or cancel its job", error=True)
        else:
            self.say(str(err), error=True)

    # ------------------------------------------------------------------ health

    @work(thread=True)
    def check_health(self) -> None:
        try:
            health = self.box.health()
            self.call_from_thread(
                self.say, f"box is up, version {health.get('version', '?')}"
            )
        except Exception as err:
            self.call_from_thread(self.fail, err)

    # ---------------------------------------------------------------- sessions

    def action_refresh(self) -> None:
        self.load_sessions()

    @work(thread=True)
    def load_sessions(self) -> None:
        try:
            found = self.box.sessions()
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        self.call_from_thread(self._fill_sessions, found)

    def _fill_sessions(self, found: list[Session]) -> None:
        self.sessions = found
        table = self.query_one("#session-table", DataTable)
        table.clear()
        for s in found:
            table.add_row(s.name, s.kind, s.status, str(s.turns), key=s.name)
        if found and self.selected is None:
            self.selected = found[0].name
            self._show_session(found[0])

    @on(DataTable.RowHighlighted, "#session-table")
    def pick_session(self, event: DataTable.RowHighlighted) -> None:
        if event.row_key is None or event.row_key.value is None:
            return
        self.selected = str(event.row_key.value)
        for s in self.sessions:
            if s.name == self.selected:
                self._show_session(s)
                break
        self.load_artifacts()

    def _show_session(self, s: Session) -> None:
        detail = {
            "name": s.name, "kind": s.kind, "status": s.status, "dir": s.dir,
            "session_id": s.session_id, "turns": s.turns, "model": s.model,
            "effort": s.effort, "permission_mode": s.permission_mode,
            "system_prompt": s.system_prompt, "repo": s.repo, "rc_url": s.rc_url,
        }
        if s.priming:
            detail["priming"] = s.priming
        self.query_one("#session-detail", Static).update(
            json.dumps({k: v for k, v in detail.items() if v not in ("", 0, None)}, indent=2)
        )

    def action_new_session(self) -> None:
        self.push_screen(NewSession(), self._create_session)

    @work(thread=True)
    def _create_session(self, spec: dict[str, Any] | None) -> None:
        if not spec:
            return
        try:
            created = self.box.create(
                spec["name"],
                repo=spec["repo"],
                system_prompt=spec["system_prompt"],
                model=spec["model"],
                effort=spec["effort"],
                permission_mode=spec["permission_mode"],
                skills=spec["skills"],
                respond_within=spec["respond_within"],
            )
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        if created.priming:
            self.call_from_thread(
                self.say, f"priming: {created.priming.get('status')} "
                          f"{created.priming.get('error', '')}"
            )
        self.call_from_thread(self.action_refresh)

    def action_delete_session(self) -> None:
        if self.selected:
            self._delete(self.selected)

    @work(thread=True)
    def _delete(self, name: str) -> None:
        try:
            self.box.delete(name)
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        self.selected = None
        self.call_from_thread(self.action_refresh)

    def action_system_prompt(self) -> None:
        if not self.selected:
            return
        self.push_screen(
            AskFor("Append to the system prompt", "be terse", "Appended, never replacing."),
            self._set_prompt,
        )

    @work(thread=True)
    def _set_prompt(self, prompt: str | None) -> None:
        if prompt is None or not self.selected:
            return
        try:
            self.box.set_system_prompt(self.selected, prompt)
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        self.call_from_thread(self.action_refresh)

    def action_put_skill(self) -> None:
        if not self.selected:
            return
        self.push_screen(
            AskFor("Add a skill to this session", "skill-name",
                   "Writes <dir>/.claude/skills/<name>/SKILL.md"),
            self._put_skill,
        )

    @work(thread=True)
    def _put_skill(self, name: str | None) -> None:
        if not name or not self.selected:
            return
        body = f"---\nname: {name}\ndescription: added from the TUI client\n---\n\n"
        try:
            written = self.box.put_skill(self.selected, name, body)
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        self.call_from_thread(self.say, f"wrote {written.get('path')}")

    # ----------------------------------------------------------------- queries

    @on(Button.Pressed, "#send")
    def send(self) -> None:
        if not self.selected:
            self.say("pick a session first", error=True)
            return
        prompt = self.query_one("#prompt", TextArea).text.strip()
        if not prompt:
            return
        declared = [
            a.strip() for a in self.query_one("#artifacts", Input).value.split(",") if a.strip()
        ]
        self._send(self.selected, prompt,
                   self.query_one("#respond_within", Input).value.strip() or "60s", declared)

    @work(thread=True)
    def _send(self, name: str, prompt: str, respond_within: str, declared: list[str]) -> None:
        try:
            answer = self.box.query(name, prompt, respond_within, artifacts=declared)
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        self.last_job = answer.job
        self.call_from_thread(self._show_answer, answer)
        self.call_from_thread(self.load_artifacts)
        self.call_from_thread(self.action_refresh)

    def _show_answer(self, answer: Answer) -> None:
        out = self.query_one("#answer", RichLog)
        if answer.pending:
            out.write(
                f"[yellow]202[/yellow] still running — job [bold]{answer.job}[/bold]\n"
                "The query keeps going; poll it or leave it and come back."
            )
            return
        if answer.error:
            out.write(f"[red]{answer.status}[/red] {answer.error}")
            return
        out.write(
            f"[green]{answer.status}[/green] "
            f"[dim]{answer.turns} turns, {answer.duration_ms}ms[/dim]\n{answer.answer}"
        )

    @on(Button.Pressed, "#stamp")
    def stamp(self) -> None:
        """Timestamp each declared name, so a re-run keeps the earlier report."""
        field = self.query_one("#artifacts", Input)
        names = [a.strip() for a in field.value.split(",") if a.strip()] or ["report.html"]
        field.value = ", ".join(timestamped(n) for n in names)

    @on(Button.Pressed, "#poll")
    def poll(self) -> None:
        if self.last_job:
            self._poll(self.last_job)
        else:
            self.say("no job yet — send a query first")

    @work(thread=True)
    def _poll(self, job_id: str) -> None:
        try:
            job = self.box.job(job_id, respond_within="30s")
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        self.call_from_thread(self._show_answer, job)
        self.call_from_thread(self.load_artifacts)

    @on(Button.Pressed, "#cancel-job")
    def cancel_job(self) -> None:
        if self.last_job:
            self._cancel(self.last_job)

    @work(thread=True)
    def _cancel(self, job_id: str) -> None:
        try:
            result = self.box.cancel_job(job_id)
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        self.call_from_thread(
            self.say,
            f"job {job_id}: {result.get('status', 'deleted')} — "
            "the session is usable again",
        )
        self.call_from_thread(self.action_refresh)

    def action_run_command(self) -> None:
        if not self.selected:
            return
        self.push_screen(
            AskFor("Run a slash command", "/clear",
                   "Only commands in the box's allowlist. /clear rotates the conversation."),
            self._run_command,
        )

    @work(thread=True)
    def _run_command(self, command: str | None) -> None:
        if not command or not self.selected:
            return
        try:
            result = self.box.command(self.selected, command, respond_within="5m")
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        self.call_from_thread(
            self.query_one("#answer", RichLog).write,
            f"[green]{command}[/green]\n{json.dumps(result, indent=2)}",
        )
        self.call_from_thread(self.action_refresh)

    # --------------------------------------------------------------- artifacts

    @work(thread=True)
    def load_artifacts(self) -> None:
        if not self.selected:
            return
        try:
            found = self.box.artifacts(self.selected)
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        self.call_from_thread(self._fill_artifacts, found)

    def _fill_artifacts(self, found: list) -> None:
        table = self.query_one("#artifact-table", DataTable)
        table.clear()
        for a in found:
            table.add_row(a.path, f"{a.size:,}", a.expires_at[11:19], key=a.path)

    def _picked_artifact(self) -> str | None:
        table = self.query_one("#artifact-table", DataTable)
        if table.row_count == 0:
            return None
        row = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
        return str(row.value) if row and row.value else None

    @on(Button.Pressed, "#fetch")
    def fetch(self) -> None:
        path = self._picked_artifact()
        if path and self.selected:
            self._fetch(self.selected, path, save=False)

    @on(Button.Pressed, "#save")
    def save(self) -> None:
        path = self._picked_artifact()
        if path and self.selected:
            self._fetch(self.selected, path, save=True)

    @work(thread=True)
    def _fetch(self, name: str, path: str, save: bool) -> None:
        try:
            content = self.box.fetch(name, path)
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        if save:
            target = Path("out") / name / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
            self.call_from_thread(self.say, f"saved {target} ({len(content):,} bytes)")
            return
        try:
            body = content.decode()
        except UnicodeDecodeError:
            body = f"<{len(content):,} bytes of binary>"
        self.call_from_thread(self.push_screen, ShowText(path, body))

    # -------------------------------------------------------------- box-level

    def action_show_commands(self) -> None:
        self._show_commands()

    @work(thread=True)
    def _show_commands(self) -> None:
        try:
            spec = self.box.commands()
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        self.call_from_thread(
            self.push_screen,
            ShowText(f"commands.yaml — {spec.get('path', '')}", json.dumps(spec, indent=2)),
        )

    def action_show_openapi(self) -> None:
        self._show_openapi()

    @work(thread=True)
    def _show_openapi(self) -> None:
        try:
            doc = self.box.openapi()
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        lines = []
        for path, item in sorted(doc.get("paths", {}).items()):
            for method in item:
                if method.upper() in {"GET", "POST", "PUT", "DELETE", "PATCH"}:
                    summary = item[method].get("summary", "")
                    lines.append(f"{method.upper():7} {path:42} {summary}")
        self.call_from_thread(
            self.push_screen,
            ShowText(f"{doc.get('info', {}).get('title', 'API')} — every endpoint",
                     "\n".join(lines)),
        )

    def action_rotate_key(self) -> None:
        self._rotate()

    @work(thread=True)
    def _rotate(self) -> None:
        try:
            key = self.box.rotate_key()
        except Exception as err:
            self.call_from_thread(self.fail, err)
            return
        self.call_from_thread(
            self.say, f"new key {key} — the old one stopped working immediately"
        )


def main() -> None:
    parser = argparse.ArgumentParser(description="Poke at a ClaudeBox API.")
    parser.add_argument(
        "--url",
        default=os.environ.get("CLAUDEBOX_URL", "http://127.0.0.1:8091"),
        help="base URL (env CLAUDEBOX_URL)",
    )
    parser.add_argument(
        "--key",
        default=os.environ.get("CLAUDEBOX_KEY", ""),
        help="bearer key (env CLAUDEBOX_KEY), from `cbx api-key show`",
    )
    args = parser.parse_args()
    if not args.key:
        parser.error("no key — pass --key or set CLAUDEBOX_KEY")

    app_holder: dict[str, ClaudeBoxTUI] = {}

    def on_call(call: Call) -> None:
        app = app_holder.get("app")
        if app is not None:
            app.log_call(call)

    box = ClaudeBox(args.url, args.key, on_call=on_call)
    app = ClaudeBoxTUI(box)
    app_holder["app"] = app
    try:
        app.run()
    finally:
        box.close()


if __name__ == "__main__":
    main()

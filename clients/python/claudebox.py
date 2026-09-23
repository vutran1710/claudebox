"""A client for the ClaudeBox API.

Requires Python 3.12 or newer.

Every endpoint, with the parts that are easy to get wrong handled once:

  * ``respond_within`` is required on a query and has no default, so this
    asks for it rather than inventing one.
  * A query answers ``200`` with the answer *or* ``202`` with a job. Both are
    normal. ``query_and_wait`` folds them into one call for the common case;
    ``query`` hands back whichever arrived for anyone who wants the choice.
  * Only files declared on a query are fetchable afterwards. Everything else
    in the session's directory stays private.

No TUI here on purpose — lift this file into a backend and it works as is.
"""

from __future__ import annotations

import time
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import PurePosixPath
from typing import Any, Iterable, Literal

import httpx

type Duration = str | int | float
"""Seconds as a number, or a Go duration string like ``"90s"`` / ``"5m"``."""


def timestamped(path: str, when: datetime | None = None) -> str:
    """Insert a UTC timestamp before the extension.

        timestamped("report.html")  ->  "report-20260923T140532Z.html"
        timestamped("out/q3.md")    ->  "out/q3-20260923T140532Z.md"

    Two queries in one session that both write ``report.html`` leave one
    report: the file is overwritten on disk and the register keeps the newer.
    Nothing on the box prevents that, and nothing should — it cannot know
    which of the two you wanted. Naming each run distinctly is the fix, and it
    belongs where the name is chosen.

    Put the returned name in the prompt *and* in ``artifacts``, so Claude
    writes the file the query declares.
    """
    stamp = (when or datetime.now(UTC)).strftime("%Y%m%dT%H%M%SZ")
    p = PurePosixPath(path)
    return str(p.with_name(f"{p.stem}-{stamp}{p.suffix}"))


class ClaudeBoxError(RuntimeError):
    """An error the server described.

    Carries the status code because several of them mean something specific:
    ``409`` is a session already running a query, ``502`` is Claude failing
    rather than the request being wrong.
    """

    def __init__(self, status: int, message: str, method: str = "", path: str = ""):
        self.status = status
        self.message = message
        self.method = method
        self.path = path
        where = f"{method} {path}: " if path else ""
        super().__init__(f"{where}{status} {message}")

    @property
    def session_busy(self) -> bool:
        """The session already has a query running — wait, or cancel its job."""
        return self.status == 409


@dataclass(frozen=True, slots=True)
class Session:
    name: str
    dir: str
    kind: Literal["headless", "interactive"]
    status: str
    turns: int = 0
    session_id: str = ""
    repo: str = ""
    rc_url: str = ""
    system_prompt: str = ""
    permission_mode: str = ""
    model: str = ""
    effort: str = ""
    priming: dict[str, Any] | None = None

    @classmethod
    def from_json(cls, raw: dict[str, Any]) -> "Session":
        known = {f for f in cls.__slots__}
        return cls(**{k: v for k, v in raw.items() if k in known})


@dataclass(frozen=True, slots=True)
class Answer:
    """What a query came to.

    ``pending`` means the window closed before Claude finished — the work is
    still running and ``job`` is how to follow it.
    """

    job: str
    status: str
    answer: str = ""
    session_id: str = ""
    turns: int = 0
    duration_ms: int = 0
    error: str = ""
    poll: str = ""

    @property
    def pending(self) -> bool:
        return self.status == "running"

    @classmethod
    def from_json(cls, raw: dict[str, Any]) -> "Answer":
        known = {f for f in cls.__slots__}
        return cls(**{k: v for k, v in raw.items() if k in known})


@dataclass(frozen=True, slots=True)
class Artifact:
    """A declared output. ``expires_at`` takes the file with it, not just the
    registration — fetch what you need before then."""

    path: str
    size: int = 0
    job: str = ""
    sha256: str = ""
    created_at: str = ""
    expires_at: str = ""

    @classmethod
    def from_json(cls, raw: dict[str, Any]) -> "Artifact":
        known = {f for f in cls.__slots__}
        return cls(**{k: v for k, v in raw.items() if k in known})


@dataclass(frozen=True, slots=True)
class Call:
    """One request, for anyone who wants to show what the client did."""

    method: str
    path: str
    status: int
    ms: int
    note: str = ""


class ClaudeBox:
    """Talks to one box.

    ``on_call`` is invoked after every request. The TUI uses it to show the
    traffic; a backend can point it at a logger or ignore it.
    """

    def __init__(
        self,
        base_url: str,
        api_key: str,
        *,
        timeout: float = 960.0,
        on_call=None,
    ):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.on_call = on_call
        # Longer than the server's own ceiling on ``respond_within`` (15
        # minutes), or a long query would be cut off at this end instead.
        self._http = httpx.Client(
            base_url=self.base_url,
            headers={"Authorization": f"Bearer {api_key}"},
            timeout=timeout,
            follow_redirects=True,
        )

    def close(self) -> None:
        self._http.close()

    def __enter__(self) -> "ClaudeBox":
        return self

    def __exit__(self, *_) -> None:
        self.close()

    # ---------------------------------------------------------------- plumbing

    def _request(self, method: str, path: str, **kw) -> httpx.Response:
        started = time.monotonic()
        response = self._http.request(method, path, **kw)
        elapsed = int((time.monotonic() - started) * 1000)
        if self.on_call:
            self.on_call(Call(method, path, response.status_code, elapsed))
        if response.status_code >= 400:
            raise ClaudeBoxError(response.status_code, _describe(response), method, path)
        return response

    def _json(self, method: str, path: str, **kw) -> Any:
        return self._request(method, path, **kw).json()

    # -------------------------------------------------------------------- meta

    def health(self) -> dict[str, Any]:
        """The one endpoint that needs no key."""
        started = time.monotonic()
        response = httpx.get(f"{self.base_url}/healthz", timeout=10)
        if self.on_call:
            self.on_call(
                Call("GET", "/healthz", response.status_code,
                     int((time.monotonic() - started) * 1000), "no auth")
            )
        response.raise_for_status()
        return response.json()

    def openapi(self) -> dict[str, Any]:
        """The API's own description, checked against its routes by a test."""
        return self._json("GET", "/openapi.json")

    def rotate_key(self) -> str:
        """Issue a new key. The old one stops working on the next request."""
        key = self._json("POST", "/auth/rotate")["api_key"]
        self.api_key = key
        self._http.headers["Authorization"] = f"Bearer {key}"
        return key

    # ---------------------------------------------------------------- commands

    def commands(self) -> dict[str, Any]:
        """The slash-command allowlist this box will run."""
        return self._json("GET", "/commands")

    def set_commands(self, spec: str | dict[str, Any]) -> dict[str, Any]:
        """Replace the allowlist. Accepts YAML text or an equivalent dict."""
        if isinstance(spec, str):
            return self._json("PUT", "/commands", content=spec.encode())
        return self._json("PUT", "/commands", json=spec)

    # ---------------------------------------------------------------- sessions

    def sessions(self) -> list[Session]:
        """Every session on the box, both kinds."""
        return [Session.from_json(s) for s in self._json("GET", "/sessions")["sessions"]]

    def session(self, name: str) -> Session:
        return Session.from_json(self._json("GET", f"/sessions/{name}"))

    def create(
        self,
        name: str,
        *,
        repo: str = "",
        system_prompt: str = "",
        permission_mode: str = "",
        model: str = "",
        effort: str = "",
        skills: Iterable[str] = (),
        respond_within: Duration | None = None,
    ) -> Session:
        """Create a headless session.

        ``model``, ``effort`` and ``permission_mode`` are fixed here because
        Claude Code treats all three as properties of a session.

        ``skills`` *invokes* those skills as turns, which costs time — and is
        usually unnecessary, since skills installed on the box are already
        available to every session. Supply ``respond_within`` alongside them.
        """
        body: dict[str, Any] = {"name": name}
        for key, value in (
            ("repo", repo),
            ("system_prompt", system_prompt),
            ("permission_mode", permission_mode),
            ("model", model),
            ("effort", effort),
        ):
            if value:
                body[key] = value
        skills = list(skills)
        if skills:
            if respond_within is None:
                raise ValueError("invoking skills needs respond_within — each one is a turn")
            body["skills"] = skills
            body["respond_within"] = respond_within
        return Session.from_json(self._json("POST", "/sessions", json=body))

    def delete(self, name: str) -> None:
        """Forget the session and its transcript. The directory survives."""
        self._request("DELETE", f"/sessions/{name}")

    def set_system_prompt(self, name: str, prompt: str) -> Session:
        return Session.from_json(
            self._json("PUT", f"/sessions/{name}/system-prompt", json={"prompt": prompt})
        )

    def put_skill(self, name: str, skill: str, markdown: str) -> dict[str, Any]:
        """Add a skill to one session, not to the whole box."""
        return self._json(
            "PUT", f"/sessions/{name}/skills/{skill}", content=markdown.encode()
        )

    # ----------------------------------------------------------------- queries

    def query(
        self,
        name: str,
        prompt: str,
        respond_within: Duration,
        *,
        artifacts: Iterable[str] = (),
    ) -> Answer:
        """Send a prompt. Returns the answer, or a pending job.

        ``artifacts`` names the files this query is expected to produce. Only
        those become fetchable afterwards — declare them or you cannot get
        them back.

        Re-running a query in one session with the same filename overwrites
        the earlier result. :func:`timestamped` is the usual answer.
        """
        body: dict[str, Any] = {"prompt": prompt, "respond_within": respond_within}
        artifacts = list(artifacts)
        if artifacts:
            body["artifacts"] = artifacts
        return Answer.from_json(self._json("POST", f"/sessions/{name}/query", json=body))

    def command(
        self,
        name: str,
        command: str,
        *,
        respond_within: Duration | None = None,
        artifacts: Iterable[str] = (),
    ) -> dict[str, Any]:
        """Run a declared slash command.

        Commands cbx performs itself — ``/clear`` — need no ``respond_within``.
        Ones forwarded to Claude do.
        """
        body: dict[str, Any] = {"command": command}
        if respond_within is not None:
            body["respond_within"] = respond_within
        artifacts = list(artifacts)
        if artifacts:
            body["artifacts"] = artifacts
        return self._json("POST", f"/sessions/{name}/command", json=body)

    def query_and_wait(
        self,
        name: str,
        prompt: str,
        *,
        respond_within: Duration = "60s",
        artifacts: Iterable[str] = (),
        give_up_after: float = 900.0,
    ) -> Answer:
        """Send a prompt and return only once there is an answer.

        The common case, folding the 200/202 split away. Anything that needs
        to react to a job id should call :meth:`query` instead.
        """
        answer = self.query(name, prompt, respond_within, artifacts=artifacts)
        if not answer.pending:
            return answer
        deadline = time.monotonic() + give_up_after
        while time.monotonic() < deadline:
            job = self.job(answer.job, respond_within="30s")
            if job.status != "running":
                return job
        raise TimeoutError(f"job {answer.job} did not finish within {give_up_after}s")

    # -------------------------------------------------------------------- jobs

    def job(self, job_id: str, *, respond_within: Duration | None = None) -> Answer:
        """Read a job. ``respond_within`` long-polls instead of spinning."""
        params = {"respond_within": respond_within} if respond_within is not None else None
        return Answer.from_json(self._json("GET", f"/jobs/{job_id}", params=params))

    def cancel_job(self, job_id: str) -> dict[str, Any]:
        """Stop a running query, or discard a finished one."""
        return self._json("DELETE", f"/jobs/{job_id}")

    # --------------------------------------------------------------- artifacts

    def artifacts(self, name: str) -> list[Artifact]:
        """What this session may hand back — declared outputs only."""
        return [
            Artifact.from_json(a)
            for a in self._json("GET", f"/sessions/{name}/artifacts")["artifacts"]
        ]

    def fetch(self, name: str, path: str) -> bytes:
        """Fetch one artifact.

        A path that was never declared, has expired, or is gone from disk all
        answer 404 alike.
        """
        return self._request("GET", f"/sessions/{name}/artifacts/{path}").content


def _describe(response: httpx.Response) -> str:
    """Pull the server's own message out, since every one names what to do."""
    try:
        body = response.json()
    except Exception:
        return response.text.strip()[:400] or response.reason_phrase
    if isinstance(body, dict) and "error" in body:
        return str(body["error"])
    return str(body)[:400]

#!/usr/bin/env python3
"""Check a built Crush with a local Agy stand-in. The shell must outlive Agy.

The command prints fresh output only after the process that requested it exits.
Changes to chat wording, model names, and job IDs should leave the check intact.
"""
import json
import os
from pathlib import Path
import re
import select
import shlex
import subprocess
import sys
import tempfile
import uuid


def emit(**fields):
    print(json.dumps({"jsonrpc": "2.0", **fields}), flush=True)


def run_command(command):
    args = {"CommandLine": command, "WaitMsBeforeAsync": 500}
    groups = json.loads((Path.home() / ".gemini/config/hooks.json").read_text())
    for group in groups.values():
        for entry in group.get("PreToolUse", []):
            if entry["matcher"] != "*" and not re.search(entry["matcher"], "run_command"):
                continue
            for hook in entry["hooks"]:
                event = {"toolCall": {"name": "run_command", "args": args}, "cwd": os.getcwd()}
                out = subprocess.check_output(shlex.split(hook["command"]), input=json.dumps(event), text=True)
                reply = json.loads(out)
                assert reply["decision"] == "allow", reply
                args.update(reply.get("overwrite", {}))
    return subprocess.check_output(args["CommandLine"], shell=True, text=True)


def agy():
    if sys.argv[1:] != ["acp"]:
        return
    for line in sys.stdin:
        req = json.loads(line)
        method = req.get("method")
        result = {}
        if method in ("session/new", "session/resume"):
            result = {"sessionId": "test-session"}
        elif method == "session/prompt":
            text = req["params"]["prompt"][0]["text"]
            if os.getenv("CRUSH_AGY_COMMAND_TURN"):
                if not Path("started").exists():
                    command = shlex.join([sys.executable, str(Path(__file__).resolve()), "work", str(os.getpid())])
                    run_command(command)
                else:
                    output = Path("finished").read_text().strip()
                    assert output and output in text, "job output missing from the follow-up"
                    with open("delivered", "a") as delivered:
                        print(output, file=delivered)
            emit(method="session/update", params={"update": {"sessionUpdate": "agent_message_chunk",
                 "content": {"type": "text", "text": "Done."}}})
            result = {"stopReason": "end_turn"}
        if "id" in req:
            emit(id=req["id"], result=result)


def work(pid):
    # The kernel signals this descriptor when the original Agy process exits.
    fd = os.pidfd_open(pid)
    with open("started", "a") as starts:
        print(os.getpid(), file=starts)
    select.select([fd], [], [])
    os.close(fd)
    token = uuid.uuid4().hex
    Path("finished").write_text(token)
    print(token)


def check(binary):
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        bindir = root / "bin"
        bindir.mkdir()
        (bindir / "agy").symlink_to(Path(__file__).resolve())
        project = root / "project"
        project.mkdir()
        (project / "crush.json").write_text(json.dumps({"options": {"notifications": "disabled",
            "disable_metrics": True, "disable_provider_auto_update": True}}))
        env = {"HOME": tmp, "PATH": f"{bindir}:/usr/bin:/bin", "CRUSH_ENGINE": "alone",
               "XDG_CACHE_HOME": str(root / "cache"), "XDG_RUNTIME_DIR": tmp}
        model = "agy-cli/gemini-3.8-flash-low"
        subprocess.run([binary, "run", "--quiet", "--model", model, "--small-model", model, "Run the check."],
                       cwd=project, env=env, check=True, timeout=60, stdin=subprocess.DEVNULL)
        assert len((project / "started").read_text().splitlines()) == 1, "command restarted"
        assert (project / "delivered").read_text().splitlines() == [(project / "finished").read_text()]
        print("One command survived the turn and delivered its output once.")


if __name__ == "__main__":
    if Path(sys.argv[0]).name == "agy":
        agy()
    elif sys.argv[1] == "work":
        work(int(sys.argv[2]))
    elif sys.argv[1] == "check":
        check("/crush")
    else:
        # The test binary and caller can live in another chat's private folder.
        # Mount only the fixture, binary and system tools into an offline sandbox.
        mounts = []
        for system in ("/usr", "/bin", "/lib", "/lib64", "/etc"):
            if Path(system).exists():
                mounts += ["--ro-bind", system, system]
        subprocess.run(["bwrap", "--die-with-parent", "--unshare-net", *mounts,
                        "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
                        "--ro-bind", str(Path(sys.argv[1]).resolve()), "/crush",
                        "--ro-bind", str(Path(__file__).resolve()), "/fixture.py",
                        "--chdir", "/tmp", "/usr/bin/python3", "/fixture.py", "check"],
                       check=True)

#!/usr/bin/env python3
"""Generate fictional coding-agent transcripts for the website and README screenshots.

Usage: gen_mock.py <fake_home> [--today YYYY-MM-DD]

Writes Claude Code, Codex CLI, Gemini CLI and OpenCode history under <fake_home> (~/.claude, ~/.codex, ~/.gemini,
~/.local/share/opencode/opencode.db).
Everything is invented: user "demo", made-up projects, prompts and files. The random seed is fixed, so
every run makes the same sessions; only the dates follow --today (default: the current date), so the
Calendar's "today" always has data.
"""
import argparse, json, os, random, sqlite3, uuid
from datetime import datetime, timedelta, timezone

ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
ap.add_argument("home", help="fake home directory to write into")
ap.add_argument("--today", help="last day with sessions, YYYY-MM-DD (default: today)")
args = ap.parse_args()

HOME = args.home
USER_HOME = "/Users/demo"
rnd = random.Random(42)
_day = datetime.strptime(args.today, "%Y-%m-%d") if args.today else datetime.now()
TODAY = datetime(_day.year, _day.month, _day.day, 16, 0, tzinfo=timezone.utc)

PROJECTS = {
    "acme-storefront": {
        "prompts": [
            "Add a wishlist button to the product card and persist it in local storage",
            "The checkout total is off by one cent with discounts, find out why",
            "Refactor the cart service to use signals instead of RxJS subjects",
            "Write unit tests for the coupon validator",
            "Make the product grid responsive on tablets",
            "Upgrade the payment SDK and fix the breaking changes",
            "Why is the home page so slow on first load?",
            "Add a dark mode toggle to the header",
        ],
        "files": ["src/app/cart/cart.service.ts", "src/app/checkout/checkout.component.ts", "src/app/product/product-card.ts",
                  "src/app/coupons/coupon-validator.ts", "src/styles/theme.css", "package.json", "src/app/app.config.ts"],
        "test": "npm test -- --watch=false", "build": "npm run build",
    },
    "weather-api": {
        "prompts": [
            "Add a /forecast/hourly endpoint with caching",
            "The integration tests are flaky in CI, stabilise them",
            "Add rate limiting per API key",
            "Document the public endpoints in the OpenAPI spec",
            "Migrate the config loader to environment variables",
            "Find the memory leak in the station poller",
        ],
        "files": ["internal/forecast/handler.go", "internal/forecast/cache.go", "internal/station/poller.go",
                  "cmd/server/main.go", "api/openapi.yaml", "internal/ratelimit/limiter.go"],
        "test": "go test ./...", "build": "go build ./cmd/server",
    },
    "pixel-notes": {
        "prompts": [
            "Add markdown export for notes",
            "Sync fails when two devices edit the same note, design a merge strategy",
            "Add keyboard shortcuts for bold and italic",
            "Fix the crash when a note has an empty title",
            "Add search with highlighting",
        ],
        "files": ["src/editor/Editor.tsx", "src/sync/merge.ts", "src/store/notes.ts", "src/export/markdown.ts", "src/search/index.ts"],
        "test": "npx vitest run", "build": "npx vite build",
    },
    "infra-terraform": {
        "prompts": [
            "Add a staging environment that mirrors production with smaller instances",
            "Why does terraform plan want to recreate the load balancer?",
            "Add alerts for 5xx rate above 2 percent",
        ],
        "files": ["modules/lb/main.tf", "envs/prod/main.tf", "envs/staging/main.tf", "modules/alerts/main.tf"],
        "test": "terraform validate", "build": "terraform plan -out plan.out",
    },
}

ANSWERS = [
    "Done. I changed {f} and the tests pass.",
    "Fixed. The root cause was in {f}; I added a test that covers it.",
    "I implemented it in {f}. Build and tests are green.",
    "Here is what I found: the problem is in {f}. I applied a fix and verified it with the test suite.",
]
THINK = ["Let me look at the relevant files first.", "I should run the tests before changing anything.",
         "The failing assertion points to the rounding logic.", "I'll check how this is wired up."]


def iso(t):
    return t.strftime("%Y-%m-%dT%H:%M:%S.") + f"{t.microsecond // 1000:03d}Z"


def w(path, recs):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        for r in recs:
            f.write(json.dumps(r) + "\n")


# ---------------- Claude Code ----------------
def claude_session(proj, start, model, n_turns, heavy=False):
    meta = PROJECTS[proj]
    cwd = f"{USER_HOME}/code/{proj}"
    sid = str(uuid.UUID(int=rnd.getrandbits(128)))
    pdir = os.path.join(HOME, ".claude", "projects", cwd.replace("/", "-"))
    recs, t = [], start
    ctx = rnd.randint(18000, 26000)
    mid = 0
    base = {"cwd": cwd, "version": "2.4.12", "gitBranch": rnd.choice(["main", "feature/" + proj.split("-")[0]])}
    prompts = rnd.sample(meta["prompts"], min(n_turns, len(meta["prompts"])))

    def call(content, out_tokens, write=None):
        nonlocal mid, ctx, t
        mid += 1
        write = write if write is not None else rnd.randint(800, 4000)
        u = {"input_tokens": rnd.randint(1, 8), "cache_creation_input_tokens": write, "cache_read_input_tokens": ctx,
             "output_tokens": out_tokens, "cache_creation": {"ephemeral_5m_input_tokens": 0, "ephemeral_1h_input_tokens": write}}
        t += timedelta(seconds=rnd.uniform(3, 14))
        ctx += write
        return {"type": "assistant", "message": {"id": f"msg_{sid[:8]}_{mid}", "model": model, "role": "assistant",
                                                  "content": content, "usage": u}, "timestamp": iso(t), **base}

    def result(tid, text, secs, err=False, extra=None):
        nonlocal t
        t += timedelta(seconds=secs)
        r = {"type": "user", "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": tid, "content": text, "is_error": err}]},
             "timestamp": iso(t), **base}
        if extra:
            r["toolUseResult"] = extra
        return r

    for ti, prompt in enumerate(prompts):
        t += timedelta(minutes=rnd.uniform(1, 12)) if ti else timedelta()
        recs.append({"type": "user", "message": {"role": "user", "content": prompt}, "timestamp": iso(t), **base})
        recs.append(call([{"type": "thinking", "thinking": rnd.choice(THINK)}], rnd.randint(80, 400)))
        files = rnd.sample(meta["files"], min(len(meta["files"]), rnd.randint(2, 5 if not heavy else 7)))
        for f in files:
            tid = f"tu_{uuid.UUID(int=rnd.getrandbits(128)).hex[:12]}"
            recs.append(call([{"type": "tool_use", "id": tid, "name": "Read", "input": {"file_path": f"{cwd}/{f}"}}], rnd.randint(40, 120)))
            body = "\n".join(f"{i+1}\t// {f} line {i+1}" for i in range(rnd.randint(40, 220)))
            recs.append(result(tid, body, rnd.uniform(0.05, 0.3)))
        tid = f"tu_{uuid.UUID(int=rnd.getrandbits(128)).hex[:12]}"
        recs.append(call([{"type": "tool_use", "id": tid, "name": "Grep", "input": {"pattern": rnd.choice(["TODO", "export class", "func New", "discount"]), "path": cwd}}], 60))
        recs.append(result(tid, "\n".join(f"{cwd}/{x}" for x in files[:3]), 0.2))
        runs = rnd.randint(1, 4 if heavy else 2)
        for k in range(runs):
            tid = f"tu_{uuid.UUID(int=rnd.getrandbits(128)).hex[:12]}"
            fail = k < runs - 1
            recs.append(call([{"type": "tool_use", "id": tid, "name": "Bash", "input": {"command": meta["test"], "description": "Run the tests"}}], rnd.randint(60, 200)))
            out = ("FAIL  2 failing\n  expected 19.99 to equal 20.00\n" if fail else "PASS  all tests passed (148 tests)\n") + "\n".join(f"  ok  case {i}" for i in range(rnd.randint(20, 120 if heavy else 40)))
            secs = rnd.uniform(20, 140 if heavy else 60)
            recs.append(result(tid, out, secs, err=fail, extra={"stdout": out, "stderr": "", "interrupted": False}))
            ef = rnd.choice(files)
            tid = f"tu_{uuid.UUID(int=rnd.getrandbits(128)).hex[:12]}"
            recs.append(call([{"type": "tool_use", "id": tid, "name": "Edit", "input": {"file_path": f"{cwd}/{ef}", "old_string": "a", "new_string": "b"}}], rnd.randint(300, 1500)))
            recs.append(result(tid, f"The file {cwd}/{ef} has been updated.", 0.1))
        if rnd.random() < (0.6 if heavy else 0.25):
            tid = f"tu_{uuid.UUID(int=rnd.getrandbits(128)).hex[:12]}"
            aid = uuid.UUID(int=rnd.getrandbits(128)).hex[:16]
            desc = rnd.choice(["Review the change for edge cases", "Search the codebase for similar usages", "Write missing tests"])
            recs.append(call([{"type": "tool_use", "id": tid, "name": "Agent", "input": {"description": desc, "prompt": desc, "subagent_type": "general-purpose"}}], 200))
            st = t
            sub, sctx = [], 9000
            sub.append({"isSidechain": True, "type": "user", "message": {"role": "user", "content": desc}, "timestamp": iso(st), "cwd": cwd})
            for j in range(rnd.randint(3, 7)):
                st += timedelta(seconds=rnd.uniform(4, 15))
                sw = rnd.randint(1000, 5000)
                stid = f"stu_{aid}_{j}"
                f = rnd.choice(meta["files"])
                sub.append({"isSidechain": True, "type": "assistant", "message": {"id": f"msg_s_{aid}_{j}", "model": "claude-sonnet-5-5", "role": "assistant",
                            "content": [{"type": "tool_use", "id": stid, "name": "Read", "input": {"file_path": f"{cwd}/{f}"}}],
                            "usage": {"input_tokens": 3, "cache_creation_input_tokens": sw, "cache_read_input_tokens": sctx, "output_tokens": rnd.randint(50, 300)}}, "timestamp": iso(st), "cwd": cwd})
                st += timedelta(seconds=0.2)
                sub.append({"isSidechain": True, "type": "user", "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": stid, "content": "file contents"}]}, "timestamp": iso(st), "cwd": cwd})
                sctx += sw
            st += timedelta(seconds=6)
            sub.append({"isSidechain": True, "type": "assistant", "message": {"id": f"msg_s_{aid}_end", "model": "claude-sonnet-5-5", "role": "assistant",
                        "content": [{"type": "text", "text": "Looks good. Two edge cases are worth a test; I listed them above."}],
                        "usage": {"input_tokens": 3, "cache_creation_input_tokens": 500, "cache_read_input_tokens": sctx, "output_tokens": 400}}, "timestamp": iso(st), "cwd": cwd})
            w(os.path.join(pdir, sid, "subagents", f"agent-{aid}.jsonl"), sub)
            with open(os.path.join(pdir, sid, "subagents", f"agent-{aid}.meta.json"), "w") as fh:
                json.dump({"agentType": "general-purpose", "description": desc, "toolUseId": tid}, fh)
            t = st
            recs.append(result(tid, "Looks good. Two edge cases are worth a test.", 1, extra={"agentId": aid, "status": "completed"}))
        recs.append(call([{"type": "text", "text": rnd.choice(ANSWERS).format(f=files[0])}], rnd.randint(150, 600), write=rnd.randint(300, 1500)))
    recs.append({"type": "ai-title", "aiTitle": prompts[0][:60]})
    w(os.path.join(pdir, sid + ".jsonl"), recs)
    return t


# ---------------- Codex ----------------
codex_index = []


def codex_session(proj, start):
    meta = PROJECTS[proj]
    cwd = f"{USER_HOME}/code/{proj}"
    sid = str(uuid.UUID(int=rnd.getrandbits(128)))
    t = start
    recs = [{"timestamp": iso(t), "type": "session_meta", "payload": {"id": sid, "cwd": cwd, "cli_version": "0.160.0"}}]
    prompts = rnd.sample(meta["prompts"], rnd.randint(1, 3))
    ctx = 12000
    for i, p in enumerate(prompts):
        tid = f"t{i}"
        t += timedelta(minutes=rnd.uniform(1, 8))
        recs += [{"timestamp": iso(t), "type": "event_msg", "payload": {"type": "task_started", "turn_id": tid}},
                 {"timestamp": iso(t), "type": "turn_context", "payload": {"turn_id": tid, "cwd": cwd, "model": "gpt-6-luna"}},
                 {"timestamp": iso(t), "type": "response_item", "payload": {"type": "message", "role": "user", "content": [{"type": "input_text", "text": p}]}}]
        for k in range(rnd.randint(2, 5)):
            t += timedelta(seconds=rnd.uniform(3, 30))
            cid = f"c{i}_{k}"
            cmd = rnd.choice([meta["test"], "rg -n TODO", f"cat {rnd.choice(meta['files'])}", meta["build"]])
            recs.append({"timestamp": iso(t), "type": "response_item", "payload": {"type": "custom_tool_call", "name": "exec", "call_id": cid,
                         "input": f"const r = await tools.exec_command({{cmd:{json.dumps(cmd)},yield_time_ms:1000}}); text(r.output)"}})
            ctx += rnd.randint(1500, 6000)
            recs.append({"timestamp": iso(t), "type": "token_usage_record", "payload": {"response_id": f"r{sid[:6]}{i}{k}",
                         "usage": {"input_tokens": ctx, "cached_input_tokens": int(ctx * 0.85), "cache_write_input_tokens": 0, "output_tokens": rnd.randint(100, 900), "reasoning_output_tokens": rnd.randint(50, 400)}}})
            t += timedelta(seconds=rnd.uniform(1, 40))
            recs.append({"timestamp": iso(t), "type": "response_item", "payload": {"type": "custom_tool_call_output", "call_id": cid,
                         "output": [{"type": "input_text", "text": f"Script completed\nWall time {rnd.uniform(0.2, 30):.1f} seconds\nOutput:\n"}, {"type": "input_text", "text": "ok\n"}]}})
        t += timedelta(seconds=5)
        ans = rnd.choice(ANSWERS).format(f=meta["files"][0])
        recs += [{"timestamp": iso(t), "type": "response_item", "payload": {"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": ans}]}},
                 {"timestamp": iso(t), "type": "event_msg", "payload": {"type": "task_complete", "turn_id": tid, "last_agent_message": ans}}]
    d = start.strftime("%Y/%m/%d")
    w(os.path.join(HOME, ".codex", "sessions", d, f"rollout-{start.strftime('%Y-%m-%dT%H-%M-%S')}-{sid}.jsonl"), recs)
    codex_index.append({"id": sid, "thread_name": prompts[0][:50]})


# ---------------- Gemini ----------------
gem_projects = {}


def gemini_session(proj, start):
    meta = PROJECTS[proj]
    cwd = f"{USER_HOME}/code/{proj}"
    gem_projects[cwd] = proj
    sid = uuid.UUID(int=rnd.getrandbits(128)).hex[:8]
    t = start
    recs = [{"sessionId": sid, "projectHash": proj, "startTime": iso(t), "kind": "main"}]
    ctx = 8000
    for i, p in enumerate(rnd.sample(meta["prompts"], rnd.randint(1, 3))):
        t += timedelta(minutes=rnd.uniform(1, 6))
        recs.append({"id": f"u{i}", "timestamp": iso(t), "type": "user", "content": [{"text": p}]})
        for k in range(rnd.randint(1, 3)):
            t += timedelta(seconds=rnd.uniform(4, 20))
            ctx += rnd.randint(1000, 4000)
            recs.append({"id": f"g{i}{k}", "timestamp": iso(t), "type": "gemini", "content": "Working on it.", "thoughts": [],
                         "tokens": {"input": ctx, "output": rnd.randint(50, 500), "cached": int(ctx * 0.7), "thoughts": rnd.randint(0, 300), "tool": 0, "total": ctx},
                         "model": "gemini-3-flash-preview",
                         "toolCalls": [{"id": f"run{i}{k}", "name": "run_shell_command", "args": {"command": meta["test"]}, "status": "success",
                                        "result": [{"functionResponse": {"id": f"run{i}{k}", "name": "run_shell_command", "response": {"output": "OK"}}}]}]})
        t += timedelta(seconds=5)
        recs.append({"id": f"g{i}end", "timestamp": iso(t), "type": "gemini", "content": rnd.choice(ANSWERS).format(f=meta["files"][0]), "thoughts": [],
                     "tokens": {"input": ctx, "output": 120, "cached": int(ctx * 0.8), "thoughts": 0, "tool": 0, "total": ctx}, "model": "gemini-3-flash-preview"})
    w(os.path.join(HOME, ".gemini", "tmp", proj, "chats", f"session-{start.strftime('%Y-%m-%dT%H-%M')}-{sid}.jsonl"), recs)


weights = {"acme-storefront": 5, "weather-api": 4, "pixel-notes": 3, "infra-terraform": 1}
names = list(weights)
for day in range(40, -1, -1):
    d = TODAY - timedelta(days=day)
    if d.weekday() >= 5 and rnd.random() < 0.7:
        continue
    for _ in range(rnd.randint(1, 4)):
        proj = rnd.choices(names, [weights[n] for n in names])[0]
        start = d.replace(hour=rnd.randint(8, 17), minute=rnd.randint(0, 59), second=0)
        if start > TODAY:
            start = TODAY - timedelta(hours=2)
        r = rnd.random()
        if r < 0.72:
            model = rnd.choices(["claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-2"], [5, 3, 1])[0]
            claude_session(proj, start, model, rnd.randint(1, 5), heavy=rnd.random() < 0.25)
        elif r < 0.88:
            codex_session(proj, start)
        else:
            gemini_session(proj, start)



# ---------------- OpenCode ----------------
# Its own random generator, so the Claude, Codex and Gemini sessions above stay the same as before.
orn = random.Random(7)


def opencode_db():
    path = os.path.join(HOME, ".local", "share", "opencode", "opencode.db")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    if os.path.exists(path):
        os.remove(path)
    db = sqlite3.connect(path)
    db.executescript("""
    CREATE TABLE session_v2 (id text PRIMARY KEY, project_id text NOT NULL, parent_id text, slug text NOT NULL, directory text NOT NULL,
      title text, version text NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL);
    CREATE TABLE session_message (id text PRIMARY KEY, session_id text NOT NULL, type text NOT NULL, seq integer NOT NULL,
      time_created integer NOT NULL, time_updated integer NOT NULL, data text NOT NULL);
    CREATE TABLE session_part (message_id text NOT NULL, session_id text NOT NULL, ordinal integer NOT NULL, type text NOT NULL,
      call_id text, time_created integer NOT NULL, time_updated integer NOT NULL, data text NOT NULL);
    """)
    return db


def ms(t):
    return int(t.timestamp() * 1000)


def opencode_session(db, proj, start, n):
    meta = PROJECTS[proj]
    cwd = f"{USER_HOME}/code/{proj}"
    sid = f"ses_{orn.getrandbits(48):012x}"
    model = orn.choice(["claude-sonnet-5-5", "claude-sonnet-5-5", "gpt-6-luna"])
    prompts = orn.sample(meta["prompts"], orn.randint(1, 3))
    t = start
    seq = 0
    ctx = 9000
    for i, p in enumerate(prompts):
        t += timedelta(minutes=orn.uniform(1, 5))
        mid = f"msg_{sid}_{i}u"
        db.execute("INSERT INTO session_message VALUES (?,?,?,?,?,?,?)", (mid, sid, "user", seq, ms(t), ms(t), json.dumps({"time": {"created": ms(t)}})))
        db.execute("INSERT INTO session_part VALUES (?,?,?,?,?,?,?,?)", (mid, sid, 0, "text", None, ms(t), ms(t), json.dumps({"text": p})))
        seq += 1
        a0 = t + timedelta(seconds=2)
        aid = f"msg_{sid}_{i}a"
        parts = [("reasoning", None, {"text": "Look at the relevant code first, then make the change and run the tests."})]
        cost = 0.0
        for k in range(orn.randint(2, 5)):
            t += timedelta(seconds=orn.uniform(5, 40))
            f = orn.choice(meta["files"])
            tool, inp = orn.choice([("read", {"filePath": f"{cwd}/{f}"}), ("edit", {"filePath": f"{cwd}/{f}"}), ("bash", {"command": meta["test"]}), ("grep", {"pattern": "TODO"})])
            end = t + timedelta(seconds=orn.uniform(0.2, 25 if tool == "bash" else 1))
            parts.append(("tool", f"call_{k}", {"tool": tool, "callID": f"call_{k}", "state": {"status": "completed", "input": inp,
                          "output": "ok" if tool != "bash" else "PASS (42 tests)", "time": {"start": ms(t), "end": ms(end)}}}))
            ctx += orn.randint(1500, 6000)
            tok = {"input": orn.randint(200, 1500), "output": orn.randint(150, 900), "reasoning": orn.randint(0, 300),
                   "cache": {"read": ctx, "write": orn.randint(1000, 5000)}}
            c = round((tok["input"] * 3 + tok["output"] * 15 + ctx * 0.3 + tok["cache"]["write"] * 3.75) / 1e6, 4)
            cost += c
            t = end
            parts.append(("step-finish", None, {"tokens": tok, "cost": c, "time": {"end": ms(t)}}))
        parts.insert(len(parts) - 1, ("text", None, {"text": orn.choice(ANSWERS).format(f=meta["files"][0])}))
        db.execute("INSERT INTO session_message VALUES (?,?,?,?,?,?,?)", (aid, sid, "assistant", seq, ms(a0), ms(t),
                   json.dumps({"time": {"created": ms(a0), "completed": ms(t)}, "modelID": model, "providerID": "anthropic" if model.startswith("claude") else "openai",
                               "cost": round(cost, 4), "path": {"cwd": cwd, "root": cwd}})))
        for k, (typ, call, data) in enumerate(parts):
            db.execute("INSERT INTO session_part VALUES (?,?,?,?,?,?,?,?)", (aid, sid, k, typ, call, ms(a0) + k, ms(a0) + k, json.dumps(data)))
        seq += 1
    db.execute("INSERT INTO session_v2 VALUES (?,?,?,?,?,?,?,?,?)", (sid, "prj_" + proj, None, f"demo-{n}", cwd, prompts[0][:60], "1.4.2", ms(start), ms(t)))


oc = opencode_db()
n = 0
for day in range(40, -1, -1):
    if orn.random() < 0.6:
        continue
    d = TODAY - timedelta(days=day)
    start = d.replace(hour=orn.randint(9, 18), minute=orn.randint(0, 59), second=0)
    if start > TODAY:
        start = TODAY - timedelta(hours=3)
    n += 1
    opencode_session(oc, orn.choice(["weather-api", "pixel-notes", "weather-api"]), start, n)
oc.commit()
oc.close()

w(os.path.join(HOME, ".codex", "session_index.jsonl"), codex_index)
os.makedirs(os.path.join(HOME, ".gemini"), exist_ok=True)
with open(os.path.join(HOME, ".gemini", "projects.json"), "w") as f:
    json.dump({"projects": gem_projects}, f)
print("ok")

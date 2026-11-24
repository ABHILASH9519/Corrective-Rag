"""Fake OpenAI-compatible chat server so the real crag binary can run its whole pipeline offline.

The grades it returns depend on keywords in the question, so each verdict can be exercised:
  "tatkal"   -> first note relevant            -> CORRECT
  "baggage"  -> every note irrelevant          -> INCORRECT
  anything else mentioning "late" -> partial   -> AMBIGUOUS
Every call sleeps a little so the UI stages are visible.
"""
import json, re, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DELAY = float(sys.argv[2]) if len(sys.argv) > 2 else 0.45


def reply(content):
    return json.dumps({"choices": [{"message": {"content": content}}]}).encode()


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        msgs = body["messages"]
        system, user = msgs[0]["content"], msgs[1]["content"]
        schema = (body.get("response_format") or {}).get("json_schema", {}).get("name")
        time.sleep(DELAY)

        if schema == "grades":
            q = user.split("\n", 1)[0].lower()
            ids = re.findall(r"^\[([^\]]+)\]$", user, re.M)
            if "tatkal" in q:
                labels = ["relevant"] + ["irrelevant"] * (len(ids) - 1)
            elif "baggage" in q:
                labels = ["irrelevant"] * len(ids)
            else:
                labels = ["partial", "partial"] + ["irrelevant"] * max(0, len(ids) - 2)
            out = {"grades": [{"id": i, "label": l} for i, l in zip(ids, labels)]}
            content = json.dumps(out)
        elif schema == "strips":
            ids = re.findall(r"^(s\d+):", user, re.M)
            content = json.dumps({"strips": [{"id": i, "keep": n < 2} for n, i in enumerate(ids)]})
        elif "Rewrite the user's question" in system:
            content = "indigo domestic check-in baggage allowance kg"
        else:
            kb = re.search(r"\[(KB:[^\]]+)\]", user)
            web = re.search(r"\[(WEB:[^\]]+)\]", user)
            if kb:
                content = f"Tatkal booking for AC classes opens at 10:00 on the day before departure, and the ticket is non-refundable once booked. [{kb.group(1)}]"
            elif web:
                content = f"IndiGo allows about 15 kg of check-in baggage on domestic economy fares. [{web.group(1)}]"
            else:
                content = "I don't know - I couldn't find reliable information to answer that."

        data = reply(content)
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()

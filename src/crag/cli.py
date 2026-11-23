"""Command line: `crag ask "<question>"` and `crag serve`."""

from __future__ import annotations

import argparse
import asyncio
import sys


def _ask(args: argparse.Namespace) -> int:
    from crag.config import require_api_key
    from crag.graph import ask, sources_of

    require_api_key()
    question = " ".join(args.question).strip()
    if not question:
        print("Please provide a question.", file=sys.stderr)
        return 2

    state = asyncio.run(ask(question))
    if args.verbose:
        print(f"Q: {question}")
        for doc in state.get("docs", []):
            label = state["labels"].get(doc.id, "?")
            print(f"  {doc.similarity:.2f}  {label:<10}  {doc.id}")
        print(f"verdict: {state['verdict']}")
        if state.get("web_query"):
            print(f"web query: {state['web_query']}")
        print()
    print(state["answer"])
    cited = sources_of(state)
    if cited:
        print("\nSources: " + ", ".join(cited))
    return 0


def _serve(args: argparse.Namespace) -> int:
    import uvicorn

    uvicorn.run("crag.api:app", host=args.host, port=args.port)
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="crag", description="Corrective RAG help desk")
    sub = parser.add_subparsers(dest="command", required=True)

    ask_p = sub.add_parser("ask", help="ask one question")
    ask_p.add_argument("question", nargs="+")
    ask_p.add_argument("-v", "--verbose", action="store_true", help="show grading and verdict")
    ask_p.set_defaults(func=_ask)

    serve_p = sub.add_parser("serve", help="run the HTTP API")
    serve_p.add_argument("--host", default="0.0.0.0")
    serve_p.add_argument("--port", type=int, default=8000)
    serve_p.set_defaults(func=_serve)

    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())

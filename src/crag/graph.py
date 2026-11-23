"""Corrective RAG graph orchestration."""
from __future__ import annotations
import asyncio, operator, re
from typing import Annotated, TypedDict
from langgraph.graph import END, START, StateGraph
from crag import evaluator, kb, llm, refine, web

ANSWER_SYSTEM = """You are a careful, evidence-grounded assistant.
Answer using only the supplied knowledge. Every factual sentence must end with
the exact source tag(s) supporting it, copied verbatim (for example [KB:policy-17]
or [WEB:https://example.org/page]). Never invent, alter or omit source tags.
If evidence does not support an answer, say you do not know. Distinguish conflicting
evidence and avoid unsupported assumptions. Be concise and directly answer."""
NO_ANSWER = "I don't know - I couldn't find reliable information to answer that."
_TAG = re.compile(r"\[(?:KB|WEB):[^\]]+\]")

class CragState(TypedDict,total=False):
    question: str
    docs: list[kb.Doc]
    labels: dict[str,str]
    verdict: str
    knowledge: Annotated[list[str],operator.add]
    web_query: str
    answer: str

async def retrieve_node(state: CragState) -> dict:
    return {"docs":await asyncio.to_thread(kb.retrieve,state["question"])}
async def grade_node(state: CragState) -> dict:
    labels=await evaluator.grade(state["question"],state["docs"])
    return {"labels":labels,"verdict":evaluator.decide(labels)}
async def refine_node(state: CragState) -> dict:
    return {"knowledge":await refine.refine(state["question"],state["docs"])}
async def web_node(state: CragState) -> dict:
    query=await web.rewrite_query(state["question"])
    return {"web_query":query,"knowledge":await web.web_search(query)}
async def generate_node(state: CragState) -> dict:
    knowledge=state.get("knowledge") or []
    if not knowledge: return {"answer":NO_ANSWER}
    user=f"Question: {state['question']}\n\nKnowledge:\n"+"\n".join(knowledge)
    return {"answer":await llm.ask_text(ANSWER_SYSTEM,user) or NO_ANSWER}
def route(state: CragState) -> list[str]:
    return {"CORRECT":["refine"],"INCORRECT":["web"],"AMBIGUOUS":["refine","web"]}[state["verdict"]]
def build_graph():
    graph=StateGraph(CragState)
    for name,node in [("retrieve",retrieve_node),("grade",grade_node),("refine",refine_node),("web",web_node),("generate",generate_node)]:
        graph.add_node(name,node)
    graph.add_edge(START,"retrieve"); graph.add_edge("retrieve","grade")
    graph.add_conditional_edges("grade",route,["refine","web"])
    graph.add_edge("refine","generate"); graph.add_edge("web","generate")
    graph.add_edge("generate",END)
    return graph.compile()
_app=None
async def ask(question: str) -> CragState:
    global _app
    if _app is None: _app=build_graph()
    return await _app.ainvoke({"question":question,"knowledge":[]})
def sources_of(state: CragState) -> list[str]:
    seen: dict[str,None]={}
    for tag in _TAG.findall(state.get("answer","")): seen[tag.strip("[]")]=None
    return list(seen)

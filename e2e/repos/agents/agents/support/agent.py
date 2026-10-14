"""Fixture: the support agent's definition (evolve never runs it)."""


def build_agent():
    return {"name": "support", "tools": ["issue_refund"], "frontend_tools": ["confirm_refund"]}

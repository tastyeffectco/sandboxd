"""Preserve Anthropic system text for LiteLLM 1.106.0's Codex bridge."""


def normalize_system_blocks(data, model_aliases):
    """Change only explicitly selected gateway aliases; never discard content."""
    if data.get("model") not in model_aliases or not isinstance(data.get("system"), list):
        return data
    texts = []
    for block in data["system"]:
        if (
            not isinstance(block, dict)
            or block.get("type") != "text"
            or not isinstance(block.get("text"), str)
            or set(block) - {"type", "text", "cache_control"}
        ):
            raise ValueError(
                "Codex gateway normalization accepts text-only system blocks; "
                "unsupported content was not discarded"
            )
        texts.append(block["text"])
    # Codex does not implement Anthropic's per-block cache_control. Keep every
    # text block, in order, as a system string that LiteLLM maps to instructions.
    # Copy the outer object so unrelated callbacks retain their original input.
    return {**data, "system": "\n\n".join(texts)}
